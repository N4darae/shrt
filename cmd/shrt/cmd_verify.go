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
	"  1  drift, a failed replay, no safe spot, a FINDING, REGRESSION or CHAIN DEFECT, a confirmed slowdown\n" +
	"  3  could not verify: a step unanswered, a restart, auth refused, a fixture reused, a stale descriptor\n"

func runVerify(ctx context.Context, args []string) (err error) {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	vars := varFlags{}
	fs.Var(vars, "var", "set a chain var as `key=value`, repeatable")
	useRun := fs.String("run", "", "diff a recorded run `id` instead of replaying; latest is the newest")
	asJSON := fs.Bool("json", false, "print the diff report as JSON")
	quiet := fs.Bool("quiet", false, "a clean replay prints its verdict line only")
	save := fs.Bool("save", true, "persist the replay record")
	build := fs.String("build", "", buildFlagUsage)
	verbose := fs.Bool("v", false, "list each change at a step not judged for a descriptor mismatch, and each request value differing only in a fixture name")
	showLatency := fs.Bool("latency", false, "list each step's latency against the safe spot's run")
	listMasked := fs.Bool("masked", false, "list every value kept out of the comparison, with both values")
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
	name, err := e.chainName(rest[0])
	if err != nil {
		return err
	}
	if err := e.knownChain(name); err != nil {
		return err
	}
	spot, err := e.store.LoadSafeSpot(name)
	if errors.Is(err, os.ErrNotExist) {
		err = fmt.Errorf("chain %s has no safe spot: nothing is confirmed at %s", name, e.store.SafeSpotPath(name))
	}
	if errors.Is(err, store.ErrMergeConflict) {
		return err
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
		asked := *useRun
		rec, err = e.store.LoadRun(name, *useRun)
		if err == nil && rec.RunID != asked {
			*useRun = rec.RunID
			fmt.Fprintf(os.Stderr, "verify: -run %s is run %s, the newest run record of %s (a verify replay counts)\n",
				asked, rec.RunID, name)
		}
		if *useRun == spot.RunID {
			fmt.Fprintf(os.Stderr,
				"verify: run %s IS the run this safe spot was made from, so this compares a recording with "+
					"itself. It is a clean control for the differ, and it is NOT evidence about the backend: "+
					"it reports no drift whatever the backend now does. Pass a LATER run id, or drop -run to "+
					"replay live.\n", *useRun)
		}
		if resolved, resolveErr := e.resolveChainNamed(rest[0], name); resolveErr == nil {
			c = resolved
		}
	} else {
		c, err = e.resolveChainNamed(rest[0], name)
		if err != nil {
			for _, o := range doctor.OrphanSafeSpots(e.cfg) {
				if o.Name == name {
					return fmt.Errorf("%w\n%s; there is no chain to replay.\n%s", err, o.Line(), o.Remedy())
				}
			}
			return err
		}
		rec, err = executeChain(ctx, e, c, withLatency(runner.Options{Vars: c.CoerceVars(vars), Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, Build: *build, KeepGoing: true}, latencyPolicy(e), spot), true)
		if err == nil {
			rec.ReplayOf = spot.RunID
		}
		if err == nil && *save {
			if _, serr := e.store.SaveRun(rec); serr != nil {
				return serr
			}
		}
	}
	if err != nil {
		return err
	}

	spot, renamedSteps := diff.RenameSpotSteps(spot, rec.Steps)
	latency := latencyFlags(e, spot, rec, latencyPolicy(e))
	report := diff.CompareMasking(spot, rec, currentVolatile(e, name))
	defer func() { writeGateSidecar(verifySidecar(e, rec, report, latency)) }()
	report.HideMasked = !*verbose
	report.DropUnsentDefaults(spot, rec, unsentDefault(e))
	report.NoteRenamedSteps(renamedSteps)
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
	varDrift, edits, unsupplied := "", []string{}, []string{}
	if len(report.RequestChanges) > 0 {
		only := map[string]any(vars)
		if *useRun != "" {
			only = nil
		}
		varDrift = varsDifferFromConfirmed(e, spot.RunID, c, rec, only, inputVars(c, report.RequestChanges))
		defaulted := func(fed map[string]bool) []string {
			if *useRun != "" {
				return nil
			}
			return varsConfirmedOtherwise(e, spot.RunID, rec, vars, fed)
		}
		if varDrift == "" {
			unsupplied = defaulted(inputVars(c, report.RequestChanges))
			if len(unsupplied) > 0 {
				varDrift = varsDifferFromConfirmed(e, spot.RunID, c, rec, nil, inputVars(c, report.RequestChanges))
			}
		}
		fedByVars := func(ch diff.Change) bool {
			fed := inputVars(c, []diff.Change{ch})
			return varsDifferFromConfirmed(e, spot.RunID, c, rec, only, fed) != "" || len(defaulted(fed)) > 0
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
		if len(unsupplied) > 0 {
			how = "this run took the chain's own value and the confirmed run ran with another, as set by -var when it was " +
				"confirmed (unless the var's default was edited since); the steps reading it are unchanged, so the chain file is not what differs"
		}
		if len(edits) > 0 {
			how = strings.TrimSuffix(how, "; the chain file is not what differs") +
				", and the chain file changed since it was confirmed (" + strings.Join(edits, ", ") + ")"
		}
		report.InputCause = fmt.Sprintf("this run's vars differ from the confirmed run's (%s), %s", varDrift, how)
	}
	unansweredStep, unansweredWhy, unanswered := unansweredOnly(rec, report)
	loss := examineSessionLoss(e, rec)
	life := examineTokenLifetime(e, rec)
	if life != nil && driftedBefore(rec, report, life.first.index) {
		life = nil
	}
	var fresh *freshRefusal
	if loss == nil {
		fresh = repeatedFreshRefusal(e, rec)
	}
	if fresh != nil && driftedBefore(rec, report, fresh.index) {
		fresh = nil
	}
	var dropped *unansweredRepeat
	if unanswered && loss == nil && fresh == nil {
		dropped = repeatedUnanswered(e, rec, unansweredStep)
	}
	var flaky *intermittentFailure
	if !report.Clean() && loss == nil && fresh == nil && dropped == nil {
		flaky = detectIntermittent(e, rec)
	}
	var reuse *fixtureReuse
	var literal *literalCollision
	if !report.Clean() {
		literal = detectLiteralCollision(e, c, rec)
		if literal == nil {
			reuse = detectFixtureReuse(e, c, rec)
		} else if driftedBefore(rec, report, literal.index) {
			literal = nil
		}
	}
	var idem, lateIdem *idempotentReplay
	if literal == nil && reuse == nil {
		if idem = detectIdempotentReplay(e, c, spot, rec, report); idem != nil && driftedBefore(rec, report, idem.index) {
			idem, lateIdem = nil, idem
		}
	}
	driftStep, driftWhy, driftAt := firstFailureIsDrift(rec)
	declared := declaredDriftChanges(e, rec, report, driftStep)
	violation := driftStep != "" && len(declared) == 0 && !report.Clean() && !driftedBefore(rec, report, driftAt) &&
		protoViolation(ctx, e, driftWhy)
	independent := independentOfDrift(c, rec, report, driftStep, driftAt)
	var nonBackend error
	headline, notVerdict := "", "this is not a verdict about the backend"
	switch {
	case life.finding():
	case life != nil && !report.Clean() && life.first.step.Status != runner.StatusPassed:
		headline = fmt.Sprintf("a %s at step %d %s", runner.TokenRefusalPhrase(life.first.r), life.first.step.Index, life.first.step.ID)
		nonBackend = exitWith(3, "could not verify %s: %s. Nothing before step %d drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend", name, life.line(), life.first.step.Index)
	case loss.finding():
	case loss != nil && !report.Clean() && !driftedBefore(rec, report, loss.index):
		headline = fmt.Sprintf("the backend likely restarted mid-run (a token it had accepted was refused at step %d %s)", loss.step.Index, loss.step.ID)
		nonBackend = exitWith(3, "could not verify %s: %s. Nothing before step %d drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend", name, loss.line(), loss.step.Index)
	case fresh != nil:
	case dropped != nil:
	case unanswered:
		headline = fmt.Sprintf("step %s never got an answer", unansweredStep)
		switch {
		case strings.Contains(unansweredWhy, transport.NoAnswerBeforeTimeout):
			headline = fmt.Sprintf("step %s was sent and got no answer before target.timeout", unansweredStep)
		case strings.Contains(unansweredWhy, "a gateway answered for the service"):
			headline = fmt.Sprintf("step %s was not answered by the service (a gateway answered for it)", unansweredStep)
		case strings.HasPrefix(unansweredWhy, "the backend refused authentication") && refusedFreshAt(rec, unansweredStep):
			headline = fmt.Sprintf("step %s was refused at authentication with a token a login in this run had just issued, "+
				"so the credentials work: this may be an auth regression", unansweredStep)
			notVerdict = "re-run to confirm: a repeat at the same step is a finding"
			if st, _ := rec.Step(unansweredStep); st.AuthRetry != runner.AuthRetryResent {
				notVerdict = "it was not re-sent, so a restart between the login and the call explains it too"
			}
		case strings.HasPrefix(unansweredWhy, "the backend refused authentication"):
			headline = fmt.Sprintf("step %s was refused at authentication", unansweredStep)
		}
		nonBackend = couldNotVerifyAfter(name, unansweredStep, unansweredWhy, rec, previousRunAttempting(e, rec, unansweredStep))
	case violation:
	case driftStep != "" && len(declared) == 0 && len(independent) == 0 && !report.Clean() && !driftedBefore(rec, report, driftAt):
		headline = fmt.Sprintf("the response at %s does not match the descriptor", driftStep)
		nonBackend = exitWith(3, "could not verify %s: the response at %s does not match the descriptor (%s)%s; %s. "+
			"Nothing before that step drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend", name, driftStep, driftWhy, unsentWritesNote(rec, driftAt), driftRemedy(ctx, e, driftWhy))
	case idem != nil && !idem.literal && !unanswered:
		headline = idem.headline()
		nonBackend = exitWith(3, "could not verify %s: %s. Nothing before that step drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend. Re-run with a fresh value: shrt verify %s %s", name, idem.line(), name, idem.fresh())
	case reuse.finding() && !driftedBefore(rec, report, reuse.index):
	case reuse != nil && !driftedBefore(rec, report, reuse.index):
		headline = fmt.Sprintf("%s at step %s", reuse.verdict(), reuse.step)
		nonBackend = exitWith(3, "could not verify %s: %s. Nothing before that step drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend. %s", name, reuse.line(), capitalized(reuse.rerun("verify", name)))
	}
	if *asJSON {
		if olderSpot != "" {
			fmt.Fprintln(os.Stderr, "verify: "+olderSpot)
		}
		if err := emitJSON(map[string]any{"run": rec, "diff": report, "latency": latency}); err != nil {
			return err
		}
	} else {
		body := &strings.Builder{}
		defer func() {
			verdict, rest := verifyVerdict(e, name, rec, report, nonBackend != nil, err, body.String())
			fmt.Print(verdict + rest)
		}()
		if nonBackend != nil {
			fmt.Fprintf(body, "could not verify %s: %s; %s (why below)\n", name, headline, notVerdict)
		}
		if olderSpot != "" && !*quiet {
			fmt.Fprintln(body, olderSpot)
		}
		if (spot.Build != "" || rec.Build != "") && !*quiet {
			fmt.Fprintf(body, "safe spot build %s, this run build %s\n", orUnknown(spot.Build), orUnknown(rec.Build))
		}
		switch {
		case nonBackend != nil:
			if list := affectedSteps(rec, report); list != "" {
				fmt.Fprintln(body, "  affected step(s), not judged: "+list)
			}
			if life != nil {
				fmt.Fprintln(body, life.label()+life.line())
			} else if loss != nil {
				fmt.Fprintln(body, "WARNING: "+loss.line())
			}
		case life.finding():
			fmt.Fprintln(body, "FINDING: "+life.line())
		case loss.finding():
			fmt.Fprintln(body, "FINDING: "+loss.line())
		case fresh != nil:
			fmt.Fprintln(body, "FINDING: "+fresh.line())
		case dropped != nil:
			fmt.Fprintln(body, "FINDING: "+dropped.line())
		case flaky.finding():
			fmt.Fprintln(body, "FINDING: "+flaky.line())
		case loss != nil:
			fmt.Fprintln(body, "WARNING: "+loss.line())
		}
		if life != nil && !life.finding() && nonBackend == nil {
			fmt.Fprintln(body, life.label()+life.line())
		}
		if nonBackend == nil && (!unanswered || anyAnswered(rec)) {
			if !*verbose && !violation && len(declared) == 0 && driftStep != "" {
				report.FoldSteps(unjudgedSteps(rec, independent, driftAt), fmt.Sprintf("its response, or one it reads, does not "+
					"match the descriptor (%s), so it is not judged; rebuild the descriptor (shrt catalog build) and re-run, "+
					"or add -v to list them", driftWhy))
			}
			if line := report.FixtureInputLine(); *verbose && line != "" {
				fmt.Fprintln(body, line)
			}
			if *quiet {
				fmt.Fprintln(body, report.QuietText())
			} else {
				fmt.Fprintln(body, report.Text())
			}
			if list := report.MaskedList(); *listMasked && list != "" {
				fmt.Fprintln(body, list)
			}
			if *showLatency {
				fmt.Fprintln(body, diff.LatencyTable(spot.Steps, rec, latencyPolicy(e)))
			}
			for _, f := range latency {
				fmt.Fprintln(body, f.Line())
			}
			switch {
			case reuse.finding():
				fmt.Fprintln(body, "FINDING: "+reuse.line())
			case reuse != nil:
				fmt.Fprintln(body, reuse.line()+"; "+reuse.rerun("verify", name))
			}
			if literal != nil {
				fmt.Fprintln(body, "CHAIN DEFECT: "+literal.line())
			}
			if idem != nil && idem.literal {
				fmt.Fprintln(body, "CHAIN DEFECT: "+idem.line())
			}
			if lateIdem != nil {
				fmt.Fprintln(body, lateIdem.note())
			}
			for _, line := range flaky.notes() {
				fmt.Fprintln(body, "note: "+line)
			}
			if violation {
				fmt.Fprintln(body, "REGRESSION: "+violationLine(e, name, driftStep, driftWhy))
			}
			if !violation && len(declared) == 0 && len(independent) > 0 {
				fmt.Fprintf(body, "note: the response at %s does not match the descriptor (%s), so it and the steps reading it are not judged; "+
					"%d change(s) at step(s) that read nothing from it are: %s; %s\n", driftStep, driftWhy, len(independent),
					describeChanges(independent), driftRemedy(ctx, e, driftWhy))
			}
			if len(declared) > 0 {
				fmt.Fprintf(body, "note: the response at %s also does not match the descriptor (%s): the fields it does not declare were "+
					"discarded and its declared fields were compared with the safe spot's, so the change(s) above are a verdict; %s\n",
					driftStep, driftWhy, driftRemedy(ctx, e, driftWhy))
			}
			if added := diff.UnorderedAdded(spot, rec); len(added) > 0 {
				fmt.Fprintf(body, "chain change since the safe spot's run: %s added, not in what was approved. An unordered list is "+
					"compared as a multiset, which can hide only a change of order, never a changed, added or removed item, so it "+
					"does not fail verify; propose a run with it (shrt confirm %s -supersede) to have it approved\n", strings.Join(added, ", "), name)
			}
		}
		if *quiet {
			for _, line := range warningLines(rec) {
				fmt.Fprintln(body, line)
			}
		} else if line := runner.UndeclaredFieldsLine(rec); line != "" {
			fmt.Fprintln(body, "warning: "+line)
		}
	}
	if life.finding() {
		return fmt.Errorf("%s: %s", name, life.line())
	}
	if loss.finding() && !driftedBefore(rec, report, loss.index) {
		return fmt.Errorf("%s: %s", name, loss.line())
	}
	if fresh != nil {
		return fmt.Errorf("%s: %s", name, fresh.line())
	}
	if dropped != nil {
		return fmt.Errorf("%s: %s", name, dropped.line())
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
	if idem != nil && idem.literal {
		return fmt.Errorf("chain defect in %s: %s", name, idem.line())
	}
	if flaky.explainsAll(report) && !violation {
		return fmt.Errorf("%s: %s", name, flaky.line())
	}
	if violation {
		return fmt.Errorf("regression: %d change(s) vs safe spot; %s", report.Counted(), violationLine(e, name, driftStep, driftWhy))
	}
	if n := len(report.Unexplained()); n > 0 && len(report.RequestChanges) > 0 {
		if report.OnlyExpectationsEdited() && n == len(report.Changes) {
			return fmt.Errorf("regression: %d change(s) vs safe spot; the expectation change since it was confirmed explains none of them", n)
		}
		if report.OnlyExpectationsEdited() {
			return fmt.Errorf("regression: %d change(s) vs safe spot are not explained by the expectation change since it was confirmed "+
				"(%d more are: the changed step's status or the steps not reached after it, where the changed expectation failed)", n, len(report.Changes)-n)
		}
		if report.OnlyChainChanged() {
			return fmt.Errorf("regression: %d change(s) vs safe spot are at steps the chain change since it was confirmed cannot affect "+
				"(not a changed, added or removed step, after no added or removed write, and reading none of those steps), so it does not "+
				"explain them (%d more it explains)", n, len(report.Changes)-n)
		}
		return fmt.Errorf("regression: %d change(s) vs safe spot are at steps whose input did not differ, that read no value the different "+
			"input changed and follow no write whose answer changed with it, so it does not explain them (%d more it explains)", n, len(report.Changes)-n)
	}
	if !report.Clean() && varDrift != "" {
		fix := "Verify without that -var to compare like with like"
		if len(unsupplied) > 0 {
			fix = "Verify with " + strings.Join(unsupplied, " ") + " to compare like with like"
		}
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
		failed := ""
		if list := report.ReorderedExpectations(); len(list) > 0 {
			failed = "; the expectation(s) reading it by position failed: " + strings.Join(list, "; ")
		}
		return fmt.Errorf("order changed: %d change(s) vs safe spot, all in list(s) holding the safe spot's items in another order (%s)%s.\n"+
			"If the rpc promises no order, declare the list unordered (unordered: [<path>] on the step or the chain) and verify again; "+
			"if it promises one, this is a regression", len(report.Changes), strings.Join(report.Reordered, ", "), failed)
	}
	if len(declared) == 0 && len(independent) > 0 {
		return fmt.Errorf("regression: %d change(s) vs safe spot at step(s) that read nothing from %s, whose response does not match the "+
			"descriptor (%s), so it and the steps reading it are not judged: %s", len(independent), driftStep, driftWhy, describeChanges(independent))
	}
	if !report.Clean() && len(declared) > 0 {
		return fmt.Errorf("regression: %d change(s) vs safe spot, including %s in the declared fields of the response at %s, "+
			"which also does not match the descriptor (%s)", report.Counted(), describeChanges(declared), driftStep, driftWhy)
	}
	if !report.Clean() {
		first, steps := firstChange(report)
		at := ""
		if first != nil {
			at = fmt.Sprintf(" at %d step(s), first %s %s", steps, first.Step, first.Path)
		}
		return fmt.Errorf("regression: %d change(s) vs safe spot%s%s; %s", report.Counted(), at, regressionShape(report), intendedChangeNext(name))
	}
	if len(report.UnapprovedRedact) > 0 {
		blanked := "the value(s) they blanked were not compared"
		if len(report.UnapprovedRedacted) == 0 {
			blanked = "they hid nothing this run, but a change under them would not be compared"
		}
		return fmt.Errorf("the replay was redacted with redact pattern(s) the safe spot's run did not have: %s; %s, "+
			"and this is not a backend change.\nRemove them from the chain and config, or, if they are intended, run the chain and propose that run\n"+
			"in place of the safe spot so a person approves the new redaction: shrt confirm %s -supersede -note \"...\"",
			strings.Join(report.UnapprovedRedact, ", "), blanked, name)
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
	return latencyFailure(name, latency, latencyPolicy(e))
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

func varsConfirmedOtherwise(e *env, spotRun string, rec *runner.Record, supplied map[string]any, fed map[string]bool) []string {
	prev, err := e.store.LoadRun(rec.Chain, spotRun)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(fed))
	for k := range fed {
		names = append(names, k)
	}
	sort.Strings(names)
	out := []string{}
	for _, k := range names {
		if _, set := supplied[k]; set {
			continue
		}
		had, inBefore := prev.Vars[k]
		now, inNow := rec.Vars[k]
		if inBefore && inNow && fmt.Sprint(had) != fmt.Sprint(now) && fmt.Sprint(had) != pathmask.MaskRedacted {
			out = append(out, fmt.Sprintf("-var %s=%v", k, had))
		}
	}
	return out
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
	steps, exports, exported := map[string]bool{}, map[string]string{}, map[string]string{}
	for _, s := range c.Steps {
		steps[s.ID] = true
		for name, path := range s.Export {
			exports[name] = s.ID
			exported[name] = strings.TrimPrefix(path, "response.")
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
					reads = append(reads, diff.Read{Step: exports[name], Path: exported[name]})
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

func protoViolation(ctx context.Context, e *env, why string) bool {
	if strings.Contains(why, "unknown field") {
		return false
	}
	matches, err := descriptorMatchesRebuild(ctx, e.cfg)
	return err == nil && matches
}

func violationLine(e *env, name, step, why string) string {
	return fmt.Sprintf("the response at %s is a body its proto cannot hold (%s), and the descriptor matches a rebuild from %q, "+
		"so it is not stale: the backend changed what it sends at that step (a value of the wrong type, or an enum value "+
		"the proto does not declare). Fix the backend, or, if the new value is intended, declare it in the proto, rebuild "+
		"with shrt catalog build and propose a passing run: shrt confirm %s -supersede", step, why, e.cfg.Descriptor.Source, name)
}

func driftRemedy(ctx context.Context, e *env, why string) string {
	sends := "a body the proto does not describe"
	if strings.Contains(why, "unknown field") {
		sends = "field(s) the proto does not declare"
	}
	matches, err := descriptorMatchesRebuild(ctx, e.cfg)
	switch {
	case err == nil && matches && sends != "field(s) the proto does not declare":
		return fmt.Sprintf("the descriptor matches a rebuild from %q, so it is not stale: the backend sends %s, "+
			"which is a backend change", e.cfg.Descriptor.Source, sends)
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

func unjudgedSteps(rec *runner.Record, independent []diff.Change, at int) []string {
	judged := map[string]bool{}
	for _, c := range independent {
		judged[c.Step] = true
	}
	out := []string{}
	for i, st := range rec.Steps {
		if st != nil && i >= at && !judged[st.ID] {
			out = append(out, st.ID)
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
	unsentWrite := false
	for i, st := range rec.Steps {
		if st == nil || i <= at {
			continue
		}
		for _, rd := range reads[st.ID] {
			if tainted[rd.Step] {
				tainted[st.ID] = true
			}
		}
		if st.Drift || st.Status == runner.StatusSkipped || unsentWrite {
			tainted[st.ID] = true
		}
		if st.Status == runner.StatusSkipped && !chain.IsReadOnlyCall(st.Call) {
			unsentWrite = true
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

func unsentWritesAfter(rec *runner.Record, at int) []string {
	out := []string{}
	for i, st := range rec.Steps {
		if st != nil && i > at && st.Status == runner.StatusSkipped && !chain.IsReadOnlyCall(st.Call) {
			out = append(out, st.ID)
		}
	}
	return out
}

func unsentWritesNote(rec *runner.Record, at int) string {
	writes := unsentWritesAfter(rec, at)
	if len(writes) == 0 {
		return ""
	}
	return fmt.Sprintf("; the write(s) %s after it were not sent, so a later step's difference may be their missing side effect "+
		"and is not independent evidence", capList(writes, 4))
}

func regressionShape(report *diff.Report) string {
	added := []string{}
	for _, c := range report.Changes {
		if c.Kind == diff.KindNotReached {
			continue
		}
		if c.Kind != diff.KindUnexpected {
			return ""
		}
		added = append(added, c.Step+" "+c.Path)
	}
	if len(added) == 0 {
		return ""
	}
	return fmt.Sprintf(", all of them response field(s) the safe spot does not have: %s", capList(added, 4))
}

func intendedChangeNext(name string) string {
	return fmt.Sprintf("if intended, a person approves a passing run: shrt confirm %s -supersede -note \"...\"", name)
}

func verifyVerdict(e *env, name string, rec *runner.Record, report *diff.Report, noVerdict bool, err error, body string) (string, string) {
	if noVerdict {
		return "", body
	}
	if err == nil {
		line := fmt.Sprintf("%s: no drift vs safe spot %s", name, report.SafeSpotID)
		lines := strings.SplitAfter(body, "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, line) {
				return strings.TrimRight(l, "\n") + "\n", strings.Join(append(lines[:i:i], lines[i+1:]...), "")
			}
		}
		return line + "\n", body
	}
	why, _, _ := strings.Cut(err.Error(), "\n")
	if kind, _, ok := strings.Cut(why, ":"); ok && len(kind) < 40 && !strings.Contains(kind, name) {
		why = kind
	}
	first, steps := firstChange(report)
	if first == nil {
		return fmt.Sprintf("%s: FAILED vs safe spot %s: %s\n", name, report.SafeSpotID, capText(why, 200)), body
	}
	if why == "regression" || why == "order changed" {
		why = report.Class(*first) + alsoClasses(report, first)
	}
	line := fmt.Sprintf("%s: DRIFT (%s), %d step(s) changed vs safe spot %s; first: %s", name, why, steps, report.SafeSpotID, changeAt(rec, *first))
	b := verifyAttribution(e, rec, report).of(first.Step, first.Path)
	if b.own != "" {
		line += "; suspect " + b.own
	}
	if req := requestLine(rec, first.Step, b); req != "" && strings.HasPrefix(why, "regression") {
		line += "\n  " + req
	} else if b.write >= 0 && !b.knock {
		line += fmt.Sprintf(", after write %s (%s)", rec.Steps[b.write].ID, shortRPC(rec.Steps[b.write].Call))
	}
	return line + "\n", body
}

func alsoClasses(report *diff.Report, first *diff.Change) string {
	class := report.Class(*first)
	byStep := map[string]string{}
	for _, status := range []bool{false, true} {
		for _, c := range report.Changes {
			if _, seen := byStep[c.Step]; seen || c.Kind == diff.KindNotReached || c.Step == first.Step || (c.Kind == diff.KindStatus) != status {
				continue
			}
			byStep[c.Step] = report.Class(c)
		}
	}
	counts := map[string]int{}
	for _, cl := range byStep {
		if cl != class {
			counts[cl]++
		}
	}
	names := make([]string, 0, len(counts))
	for cl := range counts {
		names = append(names, cl)
	}
	sort.Strings(names)
	out := ""
	for _, cl := range names {
		out += fmt.Sprintf("; also %s at %d step(s)", cl, counts[cl])
	}
	return out
}

func firstChange(report *diff.Report) (*diff.Change, int) {
	var first *diff.Change
	steps := map[string]bool{}
	for i, c := range report.Changes {
		if c.Kind == diff.KindNotReached {
			continue
		}
		steps[c.Step] = true
		if first == nil || first.Kind == diff.KindStatus && c.Kind != diff.KindStatus && c.Step == first.Step {
			first = &report.Changes[i]
		}
	}
	return first, len(steps)
}

func changeAt(rec *runner.Record, c diff.Change) string {
	rpc := ""
	if st, ok := rec.Step(c.Step); ok && st != nil {
		rpc = " (" + shortRPC(st.Call) + ")"
	}
	want, got := gatePair(c.Want, c.Got)
	return fmt.Sprintf("%s%s %s want=%s got=%s", c.Step, rpc, c.Path, want, got)
}

func compactValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "<none>"
	case string:
		return t
	}
	return exportJSON(v)
}

func capText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
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

func refusedFreshAt(rec *runner.Record, step string) bool {
	st, ok := rec.Step(step)
	return ok && runner.RefusedFreshToken(st)
}

func couldNotVerify(name, step, why string, rec *runner.Record) error {
	return couldNotVerifyAfter(name, step, why, rec, nil)
}

func couldNotVerifyAfter(name, step, why string, rec, prev *runner.Record) error {
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
		if st.ID == step && runner.RefusedFreshToken(st) && st.AuthRetry != runner.AuthRetryResent {
			return exitWith(3, "could not verify %s: step %q was refused at authentication (%s) with a token a successful "+
				"login in this run had just issued, so the credentials work: this may be an auth regression in the backend, "+
				"not a problem with the credentials. Nothing before it drifted, and %s. It was not re-sent (not a read), so a "+
				"restart between the login and this call explains it as well, and a repeat is not reported as a finding: only a "+
				"call re-sent after a fresh login and refused again is. The step record says what was tried (auth_retry, error); "+
				"re-run verify", name, step, why, past)
		}
		if st.ID == step && runner.RefusedFreshToken(st) {
			return exitWith(3, "could not verify %s: step %q was refused at authentication (%s) with a token a successful "+
				"login in this run had just issued, so the credentials work: this may be an auth regression in the backend, "+
				"not a problem with the credentials. Nothing before it drifted, and %s. The step record says what was tried "+
				"(auth_retry, error); re-run verify to confirm: a repeat at the same step is reported as a finding", name, step, why, past)
		}
	}
	remedy := unansweredRemedy(why)
	for _, st := range rec.Steps {
		if st.ID == step && runner.NotAnsweredByService(st) {
			remedy = "the service did not answer (a gateway answered unavailable for it, as during a rolling restart), " +
				"so wait until it is up and run verify again"
		}
	}
	if strings.Contains(why, transport.NoAnswerBeforeTimeout) {
		remedy = timeoutRemedy + ", and run verify again"
	}
	if st, _ := answeredAfter(rec, step); answered > 0 && unansweredKind(st) != "" {
		why = transport.StillUp(why)
		if strings.Contains(remedy, "stopped or crashed") {
			remedy = "the backend answered later steps of this run, so it did not stop; run verify again"
		}
		if was, ok := answeredIn(prev, st); ok {
			remedy += fmt.Sprintf("; run %s, the previous run that sent step %q, had it answered (%s), so this step looks "+
				"intermittent, and one failure is not reported as a finding", prev.RunID, st.ID, was)
		} else {
			remedy += fmt.Sprintf("; if the next run fails step %q the same way while answering later steps, verify reports "+
				"it as a finding about that step", st.ID)
		}
	}
	return exitWith(3, "could not verify %s: step %q never got an answer (%s); nothing before it drifted, and %s. "+
		"This is not a verdict about the backend: %s", name, step, why, past, remedy)
}

var gatewayLogin = regexp.MustCompile(`(?i)login rejected: (http_50[234]|unavailable)\b`)

func unansweredRemedy(why string) string {
	lower := strings.ToLower(why)
	switch {
	case gatewayLogin.MatchString(why):
		return "the login was answered by a gateway, not by the service (as during a rolling restart), so wait until it is up " +
			"and run verify again"
	case strings.Contains(lower, "refused authentication") || strings.Contains(lower, "login rejected") ||
		strings.Contains(lower, "unauthenticated") || strings.Contains(lower, "permission_denied"):
		return "fix the credentials it refused, and run verify again"
	case strings.Contains(lower, "closed the connection"):
		return "check the backend is up (it stopped or crashed with the request in flight) and run verify again"
	}
	return "start or reach the target, and run verify again"
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
