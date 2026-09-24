package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/doctor"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
	"github.com/N4darae/shrt/transport"
	"gopkg.in/yaml.v3"
)

func init() {
	register(&command{
		name:    "verify",
		summary: "replay a chain and diff it against its safe spot",
		run:     runVerify,
	})
}

const verifyExitCodes = "\nexit codes:\n" +
	"  0  no drift against the safe spot, and the replay passed\n" +
	"  1  drift against the safe spot, the replay did not pass, or the chain has no safe spot\n" +
	"  3  could not verify: a step never got an answer (target unreachable, connection dropped,\n" +
	"     sent but no answer before target.timeout, a Connect unavailable or a bare HTTP\n" +
	"     502/503/504 from a gateway, login or auth refused) and nothing drifted\n" +
	"     before it; a change at or after that step\n" +
	"     is not judged, so this is not a verdict about the backend; or the first failing step was\n" +
	"     refused as a uniqueness conflict on a field built from a var whose value a recorded run of\n" +
	"     this chain already used (fixture reused: re-run with a fresh -var), or that no recorded run\n" +
	"     used, so something else created the record (fixture collision: re-run with a fresh -var);\n" +
	"     or the backend refused a\n" +
	"     token it had accepted earlier in the run (it likely restarted mid-run: re-run); when the\n" +
	"     backend refused a token a login in this run had just issued, on its first use, the\n" +
	"     credentials work and it says this may be an auth regression (exit 1 as a finding when the\n" +
	"     previous run that sent that step was refused there the same way); or the first failing step\n" +
	"     failed only because its response does not match the descriptor (validate_output, drift)\n" +
	"     and nothing drifted before it\n" +
	"  1  also when the backend refused, at the same step, a token it had accepted earlier in both this\n" +
	"     run and the previous run that sent that step: not a restart, a refusal specific to that rpc;\n" +
	"     also when a fixture collision on a field built from ${uuid} or a clock value follows a\n" +
	"     previous run refused at the same step the same way: such values are unique to their run (a\n" +
	"     repeat on var values stays exit 3, since another client may use the same values);\n" +
	"     unless either run shows a restart (a call accepted when re-sent after a fresh login, data\n" +
	"     created before the refusal gone after the re-login, or a step before it that got no answer\n" +
	"     from the service), which keeps it exit 3\n" +
	"  1  also when the first failing step was refused as a uniqueness conflict on a literal field (built\n" +
	"     from no var): the chain collides with itself on every run after the first, a chain defect\n"

func runVerify(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	vars := varFlags{}
	fs.Var(vars, "var", "override a chain var, repeatable: -var key=value")
	useRun := fs.String("run", "", "diff a recorded run id instead of replaying")
	asJSON := fs.Bool("json", false, "emit the diff report as JSON")
	quiet := fs.Bool("quiet", false, "suppress per-step progress")
	save := fs.Bool("save", true, "persist the replay record")
	build := fs.String("build", "", buildFlagUsage)
	listMasked := fs.Bool("masked", false, "list every response value kept out of the comparison: under a volatile pattern, or id- or timestamp-shaped on both sides, with both values")
	setUsage(fs, "usage: shrt verify <chain> [flags]", verifyExitCodes)
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: shrt verify <chain> [flags]")
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	name := rest[0]
	if err := e.knownChain(name); err != nil {
		return err
	}
	spot, err := e.store.LoadSafeSpot(name)
	if errors.Is(err, os.ErrNotExist) {
		err = fmt.Errorf("chain %s has no safe spot: nothing is confirmed at %s", name, e.store.SafeSpotPath(name))
	}
	if err != nil {
		if e.store.HasProposal(name) {
			return fmt.Errorf("%w\na proposal for %s awaits a person's decision: shrt confirm %s -approve -by <their email> once the user says yes, or -reject", err, name, name)
		}
		return fmt.Errorf("%w\nno safe spot yet — run the chain, check the responses, propose it with 'shrt confirm %s -note \"...\"', and a person approves it", err, name)
	}
	if !spot.DigestMatches() {
		return fmt.Errorf("safe spot %s was changed after it was approved: its digest %s does not match its content, so it is not what a person approved.\n"+
			"Restore the file (from version control or %s), or re-approve: run the chain, check the responses, "+
			"propose it with 'shrt confirm %s -supersede -note \"...\"', and a person approves it",
			rel(e.cfg.Root, e.store.SafeSpotPath(name)), spot.Digest, rel(e.cfg.Root, filepath.Join(e.store.SafeSpotsDir, "archive")), name)
	}
	olderSpot := ""
	if kind := spot.DigestKind(); kind != store.DigestCurrent {
		covers := "the chain, run, target, build, volatile patterns and step records"
		if kind == store.DigestLegacy {
			covers = "step ids, calls and responses only"
		}
		olderSpot = fmt.Sprintf("safe spot %s is of the older kind: its digest covers %s, not who approved it (confirmed_by, confirmed_at, note), "+
			"so a hand edit of those is not caught. To seal the approval, propose a passing run in its place with 'shrt confirm %s -supersede -note \"...\"' and have a person approve it",
			rel(e.cfg.Root, e.store.SafeSpotPath(name)), covers, name)
	}

	var rec *runner.Record
	var c *chain.Chain
	if *useRun != "" {
		*useRun = store.RunID(*useRun)
		if *useRun == spot.RunID {
			fmt.Fprintf(os.Stderr,
				"verify: run %s IS the run this safe spot was made from, so this compares a recording with "+
					"itself. It is a clean control for the differ, and it is NOT evidence about the backend: "+
					"it reports no drift whatever the backend now does. Pass a LATER run id, or drop -run to "+
					"replay live.\n", *useRun)
		}
		rec, err = e.store.LoadRun(name, *useRun)
		if resolved, resolveErr := chain.Resolve(e.chainsDir(), name); resolveErr == nil {
			c = resolved
		}
	} else {
		c, err = chain.Resolve(e.chainsDir(), name)
		if err != nil {
			return err
		}
		rec, err = executeChain(ctx, e, c, runner.Options{Vars: c.CoerceVars(vars), Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, Build: *build, KeepGoing: true}, *quiet || *asJSON)
		if err == nil && *save {
			if _, serr := e.store.SaveRun(rec); serr != nil {
				return serr
			}
		}
	}
	if err != nil {
		return err
	}

	report := diff.CompareMasking(spot, rec, currentVolatile(e, name))
	if spotRun, err := e.store.LoadRun(name, spot.RunID); err == nil && spotRun.Redacted != nil {
		report.NoteApprovedRedact(spotRun.Redacted, rec)
		report.NoteRedactedRequests(spot, rec)
	}
	if c != nil {
		edited := diff.ChainChangesIn(spot, c, rec)
		report.RequestChanges = append(edited, diff.DropRefEdited(diff.CompareRequests(spot, rec, derivedRequestPath(c)), edited)...)
		report.RequestChanges = append(report.RequestChanges, diff.ExpectValueChanges(spot, rec, c, fixtureTemplate(c))...)
		report.SeparateInput(spot, rec, currentVolatile(e, name), requestFixtures(c))
	}
	varDrift, edits := "", []string{}
	if len(report.RequestChanges) > 0 {
		only := map[string]any(vars)
		if *useRun != "" {
			only = nil
		}
		varDrift = varsDifferFromConfirmed(e, spot.RunID, c, rec, only, inputVars(c, report.RequestChanges))
		fedByVars := func(ch diff.Change) bool {
			return varsDifferFromConfirmed(e, spot.RunID, c, rec, only, inputVars(c, []diff.Change{ch})) != ""
		}
		for _, ch := range report.ChainEdits(fedByVars) {
			edits = append(edits, ch.Step+" "+ch.Path)
		}
	}
	if varDrift != "" {
		how := "set by -var; the chain file is not what differs"
		if *useRun != "" {
			how = "as run " + rec.RunID + " was recorded"
		}
		if len(edits) > 0 {
			how = strings.TrimSuffix(how, "; the chain file is not what differs") +
				", and the chain file changed since it was confirmed (" + strings.Join(edits, ", ") + ")"
		}
		report.InputCause = fmt.Sprintf("this run's vars differ from the confirmed run's (%s), %s", varDrift, how)
	}
	unansweredStep, unansweredWhy, unanswered := unansweredOnly(rec, report)
	loss := examineSessionLoss(e, rec)
	var fresh *freshRefusal
	if loss == nil {
		fresh = repeatedFreshRefusal(e, rec)
	}
	if fresh != nil && driftedBefore(rec, report, fresh.index) {
		fresh = nil
	}
	var reuse *fixtureReuse
	var literal *literalCollision
	if !report.Clean() {
		literal = detectLiteralCollision(c, rec)
		if literal == nil {
			reuse = detectFixtureReuse(e, c, rec)
		} else if driftedBefore(rec, report, literal.index) {
			literal = nil
		}
	}
	driftStep, driftWhy, driftAt := firstFailureIsDrift(rec)
	declared := declaredDriftChanges(e, rec, report, driftStep)
	independent := independentOfDrift(c, rec, report, driftStep, driftAt)
	var nonBackend error
	headline := ""
	switch {
	case loss.finding():
	case loss != nil && !report.Clean() && !driftedBefore(rec, report, loss.index):
		headline = fmt.Sprintf("the backend likely restarted mid-run (a token it had accepted was refused at step %d %s)", loss.step.Index, loss.step.ID)
		nonBackend = exitWith(3, "could not verify %s: %s. Nothing before step %d drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend", name, loss.line(), loss.step.Index)
	case fresh != nil:
	case unanswered:
		headline = fmt.Sprintf("step %s never got an answer", unansweredStep)
		switch {
		case strings.Contains(unansweredWhy, transport.NoAnswerBeforeTimeout):
			headline = fmt.Sprintf("step %s was sent and got no answer before target.timeout", unansweredStep)
		case strings.Contains(unansweredWhy, "a gateway answered for the service"):
			headline = fmt.Sprintf("step %s was not answered by the service (a gateway answered for it)", unansweredStep)
		case strings.HasPrefix(unansweredWhy, "the backend refused authentication"):
			headline = fmt.Sprintf("step %s was refused at authentication", unansweredStep)
		}
		nonBackend = couldNotVerify(name, unansweredStep, unansweredWhy, rec)
	case driftStep != "" && len(declared) == 0 && len(independent) == 0 && !report.Clean() && !driftedBefore(rec, report, driftAt):
		headline = fmt.Sprintf("the response at %s does not match the descriptor", driftStep)
		nonBackend = exitWith(3, "could not verify %s: the response at %s does not match the descriptor (%s); %s. "+
			"Nothing before that step drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend", name, driftStep, driftWhy, driftRemedy(ctx, e, driftWhy))
	case reuse.finding() && !driftedBefore(rec, report, reuse.index):
	case reuse != nil && !driftedBefore(rec, report, reuse.index):
		headline = fmt.Sprintf("%s at step %s", reuse.verdict(), reuse.step)
		nonBackend = exitWith(3, "could not verify %s: %s. Nothing before that step drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend. Re-run with a fresh value: shrt verify %s %s", name, reuse.line(), name, reuse.fresh())
	}
	if *asJSON {
		if olderSpot != "" {
			fmt.Fprintln(os.Stderr, "verify: "+olderSpot)
		}
		if err := emitJSON(map[string]any{"run": rec, "diff": report}); err != nil {
			return err
		}
	} else {
		fmt.Println()
		if nonBackend != nil {
			fmt.Printf("could not verify %s: %s; this is not a verdict about the backend (why below)\n", name, headline)
		}
		if olderSpot != "" {
			fmt.Println(olderSpot)
		}
		if spot.Build != "" || rec.Build != "" {
			fmt.Printf("safe spot build %s, this run build %s\n", orUnknown(spot.Build), orUnknown(rec.Build))
		}
		switch {
		case nonBackend != nil:
			if list := affectedSteps(rec, report); list != "" {
				fmt.Println("  affected step(s), not judged: " + list)
			}
			if loss != nil {
				fmt.Println("WARNING: " + loss.line())
			}
		case loss.finding():
			fmt.Println("FINDING: " + loss.line())
		case fresh != nil:
			fmt.Println("FINDING: " + fresh.line())
		case loss != nil:
			fmt.Println("WARNING: " + loss.line())
		}
		if nonBackend == nil && (!unanswered || anyAnswered(rec)) {
			fmt.Println(report.Text())
			if list := report.MaskedList(); *listMasked && list != "" {
				fmt.Println(list)
			}
			switch {
			case reuse.finding():
				fmt.Println("FINDING: " + reuse.line())
			case reuse != nil:
				fmt.Println(reuse.line() + "; re-run with a fresh value: shrt verify " + name + " " + reuse.fresh())
			}
			if literal != nil {
				fmt.Println("CHAIN DEFECT: " + literal.line())
			}
			if len(declared) == 0 && len(independent) > 0 {
				fmt.Printf("note: the response at %s does not match the descriptor (%s), so it and the steps reading it are not judged; "+
					"%d change(s) at step(s) that read nothing from it are: %s; %s\n", driftStep, driftWhy, len(independent),
					describeChanges(independent), driftRemedy(ctx, e, driftWhy))
			}
			if len(declared) > 0 {
				fmt.Printf("note: the response at %s also does not match the descriptor (%s): the fields it does not declare were "+
					"discarded and its declared fields were compared with the safe spot's, so the change(s) above are a verdict; %s\n",
					driftStep, driftWhy, driftRemedy(ctx, e, driftWhy))
			}
			if added := diff.UnorderedAdded(spot, rec); len(added) > 0 {
				fmt.Printf("chain change since the safe spot's run: %s added, not in what was approved. An unordered list is "+
					"compared as a multiset, which can hide only a change of order, never a changed, added or removed item, so it "+
					"does not fail verify; propose a run with it (shrt confirm %s -supersede) to have it approved\n", strings.Join(added, ", "), name)
			}
			if report.Clean() && !report.Widened() && !report.PrincipalChanged() {
				fmt.Printf("covers the %d step(s) of this chain only; a regression in a path no safe spot exercises is not seen\n", len(spot.Steps))
			}
		}
		if *quiet {
			for _, line := range warningLines(rec) {
				fmt.Println(line)
			}
		} else if line := runner.UndeclaredFieldsLine(rec); line != "" {
			fmt.Println("warning: " + line)
		}
	}
	if loss.finding() && !driftedBefore(rec, report, loss.index) {
		return fmt.Errorf("%s: %s", name, loss.line())
	}
	if fresh != nil {
		return fmt.Errorf("%s: %s", name, fresh.line())
	}
	if reuse.finding() && nonBackend == nil && !driftedBefore(rec, report, reuse.index) {
		return fmt.Errorf("%s: %s", name, reuse.line())
	}
	if nonBackend != nil {
		return nonBackend
	}
	if literal != nil {
		return fmt.Errorf("chain defect in %s: %s", name, literal.line())
	}
	if n := len(report.Unexplained()); n > 0 && len(report.RequestChanges) > 0 {
		if report.OnlyExpectationsEdited() && n == len(report.Changes) {
			return fmt.Errorf("regression: %d change(s) vs safe spot; the expectation change since it was confirmed explains none of them", n)
		}
		if report.OnlyExpectationsEdited() {
			return fmt.Errorf("regression: %d change(s) vs safe spot are not explained by the expectation change since it was confirmed "+
				"(%d more are: the changed step's status or the steps not reached after it, where the changed expectation failed)", n, len(report.Changes)-n)
		}
		return fmt.Errorf("regression: %d change(s) vs safe spot are at steps whose input did not differ and that read no value the different "+
			"input changed, so it does not explain them (%d more it explains)", n, len(report.Changes)-n)
	}
	if !report.Clean() && varDrift != "" {
		fix := "Verify without that -var to compare like with like"
		if *useRun != "" {
			fix = "Verify a run made with the confirmed vars, or drop -run to replay the chain as it is"
		}
		if len(edits) > 0 {
			fix += ", and restore the chain's edit (" + strings.Join(edits, ", ") + ")"
		}
		return fmt.Errorf("drift with different input: %d change(s) vs safe spot, after %s because this run's vars differ from the confirmed run's (%s).\n"+
			"%s; if the new value is intended, run the chain with it until it passes,\n"+
			"and propose that run in place of the safe spot: shrt confirm %s -supersede -note \"...\"",
			len(report.Changes), report.InputSummary(), varDrift, fix, name)
	}
	if report.PrincipalChanged() {
		return fmt.Errorf("drift with different input: %d change(s) vs safe spot, and a step ran under another auth profile or principal than the confirmed run (%s).\n"+
			"Restore the step's auth and credentials; if the new principal is intended, run the chain until it passes,\n"+
			"and propose that run in place of the safe spot: shrt confirm %s -supersede -note \"...\"",
			len(report.Changes), principalChanges(report), name)
	}
	if !report.Clean() && report.OnlyChainChanged() {
		return fmt.Errorf("drift after a chain change: %d change(s) vs safe spot, after %s since it was confirmed: the chain changed, "+
			"not the input it sends.\nRestore the chain; if the edit is intended, run it until it passes,\n"+
			"and propose that run in place of the safe spot: shrt confirm %s -supersede -note \"...\"",
			len(report.Changes), report.InputSummary(), name)
	}
	if !report.Clean() && len(report.RequestChanges) > 0 {
		return fmt.Errorf("drift with different input: %d change(s) vs safe spot, after %s since it was confirmed.\n"+
			"Restore the chain's input; if the new input is intended, bring its expectations in line, run it until it passes,\n"+
			"and propose that run in place of the safe spot: shrt confirm %s -supersede -note \"...\"",
			len(report.Changes), report.InputSummary(), name)
	}
	if !report.Clean() && len(report.PrincipalUnchecked) > 0 {
		return fmt.Errorf("drift, principal not checked: %d change(s) vs safe spot, which records no auth_principal, so they are a regression only if "+
			"this run logged in as the account it was confirmed with, and shrt cannot tell.\n"+
			"Check the credentials against the ones it was confirmed with; to turn principal checking on, propose a passing run in its place:\n"+
			"shrt confirm %s -supersede -note \"...\", and a person approves it", len(report.Changes), name)
	}
	if report.OnlyReordered() {
		return fmt.Errorf("order changed: %d change(s) vs safe spot, all in list(s) holding the safe spot's items in another order (%s).\n"+
			"If the rpc promises no order, declare the list unordered (unordered: [<path>] on the step or the chain) and verify again; "+
			"if it promises one, this is a regression", len(report.Changes), strings.Join(report.Reordered, ", "))
	}
	if len(declared) == 0 && len(independent) > 0 {
		return fmt.Errorf("regression: %d change(s) vs safe spot at step(s) that read nothing from %s, whose response does not match the "+
			"descriptor (%s), so it and the steps reading it are not judged: %s", len(independent), driftStep, driftWhy, describeChanges(independent))
	}
	if !report.Clean() && len(declared) > 0 {
		return fmt.Errorf("regression: %d change(s) vs safe spot, including %s in the declared fields of the response at %s, "+
			"which also does not match the descriptor (%s)", len(report.Changes), describeChanges(declared), driftStep, driftWhy)
	}
	if !report.Clean() {
		return fmt.Errorf("regression: %d change(s) vs safe spot", len(report.Changes))
	}
	if len(report.UnapprovedRedact) > 0 {
		return fmt.Errorf("the replay was redacted with redact pattern(s) the safe spot's run did not have: %s; the value(s) they blanked were not compared, "+
			"and this is not a backend change.\nRemove them from the chain and config, or, if they are intended, run the chain and propose that run\n"+
			"in place of the safe spot so a person approves the new redaction: shrt confirm %s -supersede -note \"...\"",
			strings.Join(report.UnapprovedRedact, ", "), name)
	}
	if report.Widened() {
		return fmt.Errorf("the replay was masked with volatile pattern(s) the safe spot did not approve: %s.\n"+
			"Remove them from the chain and config, or, if they are intended, run the chain and propose that run\n"+
			"in place of the safe spot so a person approves the wider mask: shrt confirm %s -supersede -note \"...\"",
			strings.Join(report.UnapprovedVolatile, ", "), name)
	}
	if !rec.Passed() {
		return fmt.Errorf("chain %s: %s", rec.Chain, rec.Status)
	}
	return nil
}

func varsDifferFromConfirmed(e *env, spotRun string, c *chain.Chain, rec *runner.Record, only map[string]any, fed map[string]bool) string {
	var confirmed map[string]any
	if prev, err := e.store.LoadRun(rec.Chain, spotRun); err == nil {
		confirmed = prev.Vars
	} else if c != nil {
		redact := append(append([]string{}, e.cfg.Redact...), c.Redact...)
		confirmed, _ = pathmask.NewRedactor(redact).Apply(c.Vars).(map[string]any)
	} else {
		return ""
	}
	names := []string{}
	for k := range rec.Vars {
		names = append(names, k)
	}
	for k := range confirmed {
		if _, ok := rec.Vars[k]; !ok {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	out := []string{}
	for _, k := range names {
		if _, supplied := only[k]; only != nil && !supplied {
			continue
		}
		if fed != nil && !fed[k] {
			continue
		}
		now, had := rec.Vars[k], confirmed[k]
		_, inNow := rec.Vars[k]
		_, inBefore := confirmed[k]
		switch {
		case inNow && !inBefore:
			out = append(out, fmt.Sprintf("%s=%v, confirmed without it", k, now))
		case !inNow && inBefore:
			out = append(out, fmt.Sprintf("%s unset, confirmed with %v", k, had))
		case fmt.Sprint(now) != fmt.Sprint(had):
			out = append(out, fmt.Sprintf("%s=%v, confirmed with %v", k, now, had))
		}
	}
	return strings.Join(out, "; ")
}

func principalChanges(report *diff.Report) string {
	out := []string{}
	for _, c := range report.RequestChanges {
		switch c.Path {
		case diff.AuthProfilePath:
			out = append(out, fmt.Sprintf("%s: %v -> %v", c.Step, c.Want, c.Got))
		case diff.AuthPrincipalPath:
			out = append(out, fmt.Sprintf("%s: principal %v -> %v", c.Step, c.Want, c.Got))
		}
	}
	return strings.Join(out, ", ")
}

func orUnknown(s string) string {
	if s == "" {
		return "(not recorded)"
	}
	return s
}

var requestRef = regexp.MustCompile(`\$\{\s*([^}]*)\}`)

func requestTemplate(c *chain.Chain, step, path string) (any, bool) {
	s, ok := c.Step(step)
	if !ok {
		return nil, false
	}
	if name, isHeader := strings.CutPrefix(path, diff.HeadersPathPrefix); isHeader {
		for k, v := range s.Headers {
			if strings.EqualFold(k, name) {
				return v, true
			}
		}
		return nil, false
	}
	return chain.Get(s.Body, path)
}

func fixtureRequestPath(c *chain.Chain) func(step, path string) bool {
	isolating := isolationVars(c)
	return func(step, path string) bool {
		v, ok := requestTemplate(c, step, path)
		if !ok {
			return false
		}
		text, ok := v.(string)
		if !ok {
			return false
		}
		refs := requestRef.FindAllStringSubmatch(text, -1)
		if len(refs) == 0 || !namedAround(text) {
			return false
		}
		for _, m := range refs {
			n := varName.FindStringSubmatch(strings.TrimSpace(m[1]))
			if n == nil || !isolating[n[1]] {
				return false
			}
		}
		return true
	}
}

func fixtureTemplate(c *chain.Chain) func(string) bool {
	isolating := isolationVars(c)
	return func(text string) bool {
		refs := requestRef.FindAllStringSubmatch(text, -1)
		if len(refs) == 0 || !namedAround(text) {
			return false
		}
		for _, m := range refs {
			n := varName.FindStringSubmatch(strings.TrimSpace(m[1]))
			if n == nil || !isolating[n[1]] {
				return false
			}
		}
		return true
	}
}

func namedAround(text string) bool {
	rest := strings.TrimSpace(requestRef.ReplaceAllString(text, ""))
	return strings.Trim(rest, "0123456789.+-") != ""
}

func isolationVars(c *chain.Chain) map[string]bool {
	named, numeric := map[string]bool{}, map[string]bool{}
	var visit func(v any, path string)
	visit = func(v any, path string) {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				visit(x, pathmask.Join(path, k))
			}
		case []any:
			for i, x := range t {
				visit(x, pathmask.Join(path, pathmask.IndexKey(i)))
			}
		case string:
			refs := requestRef.FindAllStringSubmatch(t, -1)
			if len(refs) == 0 || strings.TrimSpace(requestRef.ReplaceAllString(t, "")) == "" {
				return
			}
			for _, m := range refs {
				n := varName.FindStringSubmatch(strings.TrimSpace(m[1]))
				switch {
				case n == nil:
				case !namedAround(t):
					numeric[n[1]] = true
				case !diff.IDNamedPath(path):
					named[n[1]] = true
				}
			}
		}
	}
	for _, s := range c.Steps {
		if s != nil {
			visit(s.Body, "")
			for name, value := range s.Headers {
				visit(value, diff.HeadersPathPrefix+name)
			}
		}
	}
	out := map[string]bool{}
	for v := range named {
		if !numeric[v] {
			out[v] = true
		}
	}
	return out
}

func requestFixtures(c *chain.Chain) diff.Fixtures {
	return diff.Fixtures{Named: fixtureRequestPath(c), Generated: generatedRequestPath(c), Var: fixtureOnlyVar(c), Reads: chainReads(c)}
}

func chainReads(c *chain.Chain) map[string][]diff.Read {
	if c == nil {
		return nil
	}
	steps, exports := map[string]bool{}, map[string]string{}
	for _, s := range c.Steps {
		steps[s.ID] = true
		for name := range s.Export {
			exports[name] = s.ID
		}
	}
	out := map[string][]diff.Read{}
	for _, s := range c.Steps {
		raw, err := json.Marshal(s)
		var doc any
		if err != nil || json.Unmarshal(raw, &doc) != nil {
			continue
		}
		reads := []diff.Read{}
		visitStrings(doc, func(text string) {
			for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
				ref := chain.ParseRef(m[1])
				if name, ok := ref.ExportName(); ok && exports[name] != "" && !(ref.Kind == chain.RefBare && steps[ref.Head]) {
					reads = append(reads, diff.Read{Step: exports[name]})
					continue
				}
				id, ok := ref.StepID()
				if !ok || !steps[id] {
					continue
				}
				if rest, isRequest := strings.CutPrefix(ref.Rest, "request."); isRequest || ref.Rest == "request" {
					reads = append(reads, diff.Read{Step: id, Request: true, Path: rest})
					continue
				}
				reads = append(reads, diff.Read{Step: id, Path: strings.TrimPrefix(ref.Rest, "response.")})
			}
		})
		out[s.ID] = reads
	}
	return out
}

func fixtureOnlyVar(c *chain.Chain) func(string) bool {
	isolating := isolationVars(c)
	fixture := fixtureTemplate(c)
	disqualified := map[string]bool{}
	var visit func(v any)
	visit = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for _, x := range t {
				visit(x)
			}
		case []any:
			for _, x := range t {
				visit(x)
			}
		case string:
			refs := requestRef.FindAllStringSubmatch(t, -1)
			if len(refs) == 0 || fixture(t) {
				return
			}
			for _, m := range refs {
				if n := varName.FindStringSubmatch(strings.TrimSpace(m[1])); n != nil {
					disqualified[n[1]] = true
				}
			}
		}
	}
	raw, err := c.Marshal()
	var doc map[string]any
	if err != nil || yaml.Unmarshal(raw, &doc) != nil {
		return func(string) bool { return false }
	}
	delete(doc, "vars")
	visit(doc)
	return func(name string) bool { return isolating[name] && !disqualified[name] }
}

func generatedRequestPath(c *chain.Chain) func(step, path string) bool {
	return func(step, path string) bool {
		v, ok := requestTemplate(c, step, path)
		if !ok {
			return false
		}
		text, ok := v.(string)
		if !ok {
			return false
		}
		for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
			if k := chain.ParseRef(m[1]).Kind; k == chain.RefUUID || k == chain.RefClock {
				return true
			}
		}
		return false
	}
}

var varName = regexp.MustCompile(`^vars\.([A-Za-z0-9_-]+)`)

func inputVars(c *chain.Chain, changes []diff.Change) map[string]bool {
	if c == nil {
		return nil
	}
	out := map[string]bool{}
	var collect func(v any)
	collect = func(v any) {
		switch t := v.(type) {
		case string:
			for _, m := range requestRef.FindAllStringSubmatch(t, -1) {
				if n := varName.FindStringSubmatch(strings.TrimSpace(m[1])); n != nil {
					out[n[1]] = true
				}
			}
		case map[string]any:
			for _, x := range t {
				collect(x)
			}
		case []any:
			for _, x := range t {
				collect(x)
			}
		}
	}
	for _, ch := range changes {
		if ch.Path == diff.ExpectValuePath {
			collect(ch.Detail)
			continue
		}
		if v, ok := requestTemplate(c, ch.Step, ch.Path); ok {
			collect(v)
		}
	}
	return out
}

func derivedRequestPath(c *chain.Chain) func(step, path string) bool {
	return func(step, path string) bool {
		v, ok := requestTemplate(c, step, path)
		if !ok {
			return false
		}
		text, ok := v.(string)
		if !ok {
			return false
		}
		for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
			head, _, _ := strings.Cut(strings.TrimSpace(m[1]), ".")
			if head != "vars" && head != "env" {
				return true
			}
		}
		return false
	}
}

func anyAnswered(rec *runner.Record) bool {
	for _, st := range rec.Steps {
		if answeredByService(st) {
			return true
		}
	}
	return false
}

func answeredByService(st *runner.StepRecord) bool {
	return st != nil && (st.HTTPStatus != 0 || len(st.Response) > 0) && !runner.NotAnsweredByService(st)
}

var descriptorMismatch = regexp.MustCompile(`does not match [^:\s]+: (?:proto:[\s\x{00a0}]+)?(?:\(line [^)]*\):[\s\x{00a0}]+)?([^\n]*)`)

func firstFailureIsDrift(rec *runner.Record) (string, string, int) {
	for i, st := range rec.Steps {
		if st == nil || st.Status == runner.StatusPassed || st.Status == runner.StatusSkipped {
			continue
		}
		if !st.Drift {
			return "", "", 0
		}
		why := "its body does not decode as the response message"
		if m := descriptorMismatch.FindStringSubmatch(st.Error); m != nil && strings.TrimSpace(m[1]) != "" {
			why = strings.TrimSpace(m[1])
		}
		return st.ID, why, i
	}
	return "", "", 0
}

var descriptorMatchesRebuild = doctor.DescriptorMatchesRebuild

func driftRemedy(ctx context.Context, e *env, why string) string {
	sends := "a body the proto does not describe"
	if strings.Contains(why, "unknown field") {
		sends = "field(s) the proto does not declare"
	}
	matches, err := descriptorMatchesRebuild(ctx, e.cfg)
	switch {
	case err == nil && matches:
		return fmt.Sprintf("the descriptor matches a rebuild from %q, so rebuilding changes nothing: the backend sends %s. "+
			"Declare them in the proto if they are intended, or turn validate_output off", e.cfg.Descriptor.Source, sends)
	case err == nil:
		return fmt.Sprintf("the descriptor does not match a rebuild from %q: rebuild it with shrt catalog build, or turn validate_output off", e.cfg.Descriptor.Source)
	}
	return "if shrt doctor says the descriptor is stale, rebuild it with shrt catalog build; if it matches a rebuild, the backend sends " +
		sends + ": declare them in the proto, or turn validate_output off"
}

func declaredDriftChanges(e *env, rec *runner.Record, report *diff.Report, step string) []diff.Change {
	if step == "" || e.cat == nil {
		return nil
	}
	st, ok := rec.Step(step)
	if !ok || len(st.Response) == 0 {
		return nil
	}
	m, err := e.cat.Lookup(st.Procedure)
	if err != nil {
		return nil
	}
	if _, err := e.cat.Canonicalize(m.Output(), st.Response); err != nil {
		return nil
	}
	out := []diff.Change{}
	for _, c := range report.Changes {
		if c.Step == step && c.Kind != diff.KindStatus && c.Kind != diff.KindNotReached {
			out = append(out, c)
		}
	}
	return out
}

func independentOfDrift(c *chain.Chain, rec *runner.Record, report *diff.Report, step string, at int) []diff.Change {
	if step == "" || c == nil {
		return nil
	}
	reads := chainReads(c)
	tainted := map[string]bool{step: true}
	after := map[string]bool{}
	for i, st := range rec.Steps {
		if st == nil || i <= at {
			continue
		}
		for _, rd := range reads[st.ID] {
			if tainted[rd.Step] {
				tainted[st.ID] = true
			}
		}
		if st.Drift || st.Status == runner.StatusSkipped {
			tainted[st.ID] = true
		}
		after[st.ID] = true
	}
	out := []diff.Change{}
	for _, ch := range report.Changes {
		if after[ch.Step] && !tainted[ch.Step] && ch.Kind != diff.KindNotReached && ch.Kind != diff.KindStatus {
			out = append(out, ch)
		}
	}
	return out
}

func describeChanges(changes []diff.Change) string {
	out := []string{}
	for _, c := range changes {
		out = append(out, fmt.Sprintf("%s %s %v -> %v", c.Step, c.Path, c.Want, c.Got))
	}
	return capList(out, 4)
}

const timeoutRemedy = "the request was sent and no answer came before target.timeout, so raise target.timeout in .shrt/config.yaml " +
	"or find why the backend answers so slowly"

func timedOutStep(rec *runner.Record) string {
	for _, st := range rec.Steps {
		if st != nil && strings.Contains(st.Error, transport.NoAnswerBeforeTimeout) {
			return st.ID
		}
	}
	return ""
}

func couldNotVerify(name, step, why string, rec *runner.Record) error {
	after, answered := false, 0
	for _, st := range rec.Steps {
		if after && answeredByService(st) {
			answered++
		}
		after = after || st.ID == step
	}
	past := "nothing after it got an answer"
	if answered > 0 {
		past = fmt.Sprintf("the %d step(s) after it that got an answer were compared, but a change at or after it is "+
			"not judged, since the unanswered call may explain it", answered)
	}
	for _, st := range rec.Steps {
		if st.ID == step && runner.RefusedFreshToken(st) {
			return exitWith(3, "could not verify %s: step %q was refused at authentication (%s) with a token a successful "+
				"login in this run had just issued, so the credentials work: this may be an auth regression in the backend, "+
				"not a problem with the credentials. Nothing before it drifted, and %s. The step record says what was tried "+
				"(auth_retry, error); re-run verify to confirm: a repeat at the same step is reported as a finding", name, step, why, past)
		}
	}
	remedy := "start or reach the target, or fix the credentials it refused, and run verify again"
	for _, st := range rec.Steps {
		if st.ID == step && runner.NotAnsweredByService(st) {
			remedy = "the service did not answer (a gateway answered unavailable for it, as during a rolling restart), " +
				"so wait until it is up and run verify again"
		}
	}
	if strings.Contains(why, transport.NoAnswerBeforeTimeout) {
		remedy = timeoutRemedy + ", and run verify again"
	}
	return exitWith(3, "could not verify %s: step %q never got an answer (%s); nothing before it drifted, and %s. "+
		"This is not a verdict about the backend: %s", name, step, why, past, remedy)
}

func unansweredOnly(rec *runner.Record, report *diff.Report) (string, string, bool) {
	unanswered := map[string]string{}
	first := ""
	index := map[string]int{}
	refusedFrom := -1
	for i, st := range rec.Steps {
		index[st.ID] = i
		authRefused := st.Status == runner.StatusError && st.AuthRetry != ""
		gateway := runner.NotAnsweredByService(st)
		if (st.Status == runner.StatusError && st.HTTPStatus == 0 && len(st.Response) == 0) || authRefused || gateway {
			why := st.Error
			if gateway {
				why, _, _ = strings.Cut(st.Error, "\n")
				why = fmt.Sprintf("HTTP %d %s: a gateway answered for the service", st.HTTPStatus, why)
			} else if authRefused {
				line, _, _ := strings.Cut(st.Error, "\n")
				why = "the backend refused authentication: " + line
			}
			unanswered[st.ID] = why
			if first == "" {
				first = st.ID
				refusedFrom = i
			}
		}
	}
	if first == "" {
		return "", "", false
	}
	for _, c := range report.Changes {
		if c.Kind == diff.KindNotReached {
			continue
		}
		if _, skip := unanswered[c.Step]; skip {
			continue
		}
		if i, ok := index[c.Step]; refusedFrom >= 0 && (!ok || i >= refusedFrom) {
			continue
		}
		return "", "", false
	}
	return first, unanswered[first], true
}

func affectedSteps(rec *runner.Record, report *diff.Report) string {
	status := map[string]string{}
	for _, st := range rec.Steps {
		if st != nil {
			status[st.ID] = st.Status
		}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, c := range report.Changes {
		if c.Step == "" || seen[c.Step] {
			continue
		}
		seen[c.Step] = true
		st := status[c.Step]
		if st == "" {
			st = "not run"
		}
		out = append(out, c.Step+" ("+st+")")
	}
	return capList(out, 6)
}
