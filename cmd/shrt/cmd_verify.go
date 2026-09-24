package main

import (
	"context"
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
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
	"github.com/N4darae/shrt/transport"
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
	"     credentials work and it says this may be an auth regression\n" +
	"  1  also when the backend refused, at the same step, a token it had accepted earlier in both this\n" +
	"     run and the previous run that sent that step: not a restart, a refusal specific to that rpc\n"

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
	var reuse *fixtureReuse
	if !report.Clean() {
		reuse = detectFixtureReuse(e, c, rec)
	}
	var nonBackend error
	headline := ""
	switch {
	case loss.finding():
	case loss != nil && !report.Clean() && !driftedBefore(rec, report, loss.index):
		headline = fmt.Sprintf("the backend likely restarted mid-run (a token it had accepted was refused at step %d %s)", loss.step.Index, loss.step.ID)
		nonBackend = exitWith(3, "could not verify %s: %s. Nothing before step %d drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend", name, loss.line(), loss.step.Index)
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
		case loss.finding():
			fmt.Println("FINDING: " + loss.line())
		case loss != nil:
			fmt.Println("WARNING: " + loss.line())
		}
		if nonBackend == nil && (!unanswered || anyAnswered(rec)) {
			fmt.Println(report.Text())
			if list := report.MaskedList(); *listMasked && list != "" {
				fmt.Println(list)
			}
			if reuse != nil {
				fmt.Println(reuse.line() + "; re-run with a fresh value: shrt verify " + name + " " + reuse.fresh())
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
	if nonBackend != nil {
		return nonBackend
	}
	if n := len(report.Unexplained()); n > 0 && len(report.RequestChanges) > 0 {
		if report.OnlyExpectationsEdited() && n == len(report.Changes) {
			return fmt.Errorf("regression: %d change(s) vs safe spot; the expectation change since it was confirmed explains none of them", n)
		}
		if report.OnlyExpectationsEdited() {
			return fmt.Errorf("regression: %d change(s) vs safe spot are not explained by the expectation change since it was confirmed "+
				"(%d more are: the changed step's status or the steps not reached after it, where the changed expectation failed)", n, len(report.Changes)-n)
		}
		return fmt.Errorf("regression: %d change(s) vs safe spot come before any step whose input differs, so the different input does not explain them "+
			"(%d more come at or after it)", n, len(report.Changes)-n)
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
	if !report.Clean() {
		return fmt.Errorf("regression: %d change(s) vs safe spot", len(report.Changes))
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
	return diff.Fixtures{Named: fixtureRequestPath(c), Generated: generatedRequestPath(c)}
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
		if st.HTTPStatus != 0 || len(st.Response) > 0 {
			return true
		}
	}
	return false
}

func couldNotVerify(name, step, why string, rec *runner.Record) error {
	after, answered := false, 0
	for _, st := range rec.Steps {
		if after && (st.HTTPStatus != 0 || len(st.Response) > 0) {
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
				"(auth_retry, error); re-run verify to confirm, and treat a repeat as a finding", name, step, why, past)
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
		remedy = "the request was sent and no answer came before target.timeout, so raise target.timeout in .shrt/config.yaml " +
			"or find why the backend answers so slowly, and run verify again"
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
