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

type verification struct {
	e                                                              *env
	vars                                                           varFlags
	name, arg, useRun, build, olderSpot, varDrift, headline        string
	unansweredStep, unansweredWhy, driftStep, driftWhy, notVerdict string
	asJSON, quiet, save, verbose, showLatency, listMasked          bool
	unanswered, violation, runToo, flakyOnly                       bool
	driftAt                                                        int
	edits, unsupplied                                              []string
	spot                                                           *store.SafeSpot
	rec                                                            *runner.Record
	c                                                              *chain.Chain
	report                                                         *diff.Report
	latency                                                        []diff.LatencyFlag
	declared, independent                                          []diff.Change
	nonBackend                                                     error
	life                                                           *tokenLifetime
	loss                                                           *sessionLoss
	fresh                                                          *freshRefusal
	dropped                                                        *unansweredRepeat
	flaky                                                          *intermittentFailure
	reuse                                                          *fixtureReuse
	literal                                                        *literalCollision
	idem, lateIdem                                                 *idempotentReplay
	notes                                                          []string
}

func runVerify(ctx context.Context, args []string) (err error) {
	v := &verification{vars: varFlags{}}
	defer func() { writeGateSidecar(v.sidecar(), err) }()
	if err := v.parse(args); err != nil {
		return err
	}
	if err := v.load(ctx); err != nil {
		return err
	}
	v.compare()
	v.explainInput()
	v.examine(ctx)
	v.notJudged(ctx)
	if v.asJSON {
		if v.olderSpot != "" {
			fmt.Fprintln(os.Stderr, "verify: "+v.olderSpot)
		}
		if err := emitJSON(map[string]any{"run": v.rec, "diff": v.report, "latency": v.latency}); err != nil {
			return err
		}
		return v.verdict(ctx)
	}
	body := &strings.Builder{}
	defer func() {
		verdict, rest := verifyVerdict(v.e, v.name, v.rec, v.report, v.flaky, v.nonBackend != nil, err, body.String())
		fmt.Print(verdict + rest)
		if first, _ := firstChange(v.report, v.rec); err != nil && v.nonBackend == nil && first != nil {
			err = shownError{err}
		}
	}()
	v.writeBody(ctx, body)
	return v.verdict(ctx)
}

func (v *verification) parse(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.Var(v.vars, "var", "set a chain var as `key=value`, repeatable")
	fs.StringVar(&v.useRun, "run", "", "diff a recorded run `id` instead of replaying; latest is the newest")
	fs.BoolVar(&v.asJSON, "json", false, "print the diff report as JSON")
	fs.BoolVar(&v.quiet, "quiet", false, "a clean replay prints its verdict line only")
	fs.BoolVar(&v.save, "save", true, "persist the replay record")
	fs.StringVar(&v.build, "build", "", buildFlagUsage)
	fs.BoolVar(&v.verbose, "v", false, "list each change at a step not judged for a descriptor mismatch, and say what an intermittent or repeated FINDING means")
	fs.BoolVar(&v.showLatency, "latency", false, "list each step's latency against the safe spot's run")
	fs.BoolVar(&v.listMasked, "masked", false, "list every value kept out of the comparison, with both values, and the targets when they differ")
	setUsage(fs, "usage: shrt verify <chain> [flags]", verifyExitCodes)
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: shrt verify <chain> [flags]")
	}
	v.arg = rest[0]
	return nil
}

func (v *verification) load(ctx context.Context) error {
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	v.e = e
	if v.name, err = e.chainName(v.arg); err != nil {
		return err
	}
	if err := e.knownChain(v.name); err != nil {
		return err
	}
	if v.spot, v.olderSpot, err = loadVerifySpot(e, v.name); err != nil {
		return err
	}
	if v.useRun != "" {
		return v.loadRun()
	}
	return v.replay(ctx)
}

func loadVerifySpot(e *env, name string) (*store.SafeSpot, string, error) {
	spot, err := e.store.LoadSafeSpot(name)
	if errors.Is(err, os.ErrNotExist) {
		err = fmt.Errorf("chain %s has no safe spot: nothing is confirmed at %s", name, e.store.SafeSpotPath(name))
	}
	if errors.Is(err, store.ErrMergeConflict) {
		return nil, "", err
	}
	if err != nil {
		if e.store.HasProposal(name) {
			return nil, "", fmt.Errorf("%w\na proposal for %s awaits a person's decision: shrt confirm %s -approve -by <their email> once the user says yes, or -reject", err, name, name)
		}
		if c, cerr := chain.Resolve(e.chainsDir(), name); cerr == nil && len(c.KeptRed) > 0 {
			return nil, "", fmt.Errorf("chain %s is kept red on purpose, so it has no safe spot by design: shrt gate compares its pins; check it with 'shrt run %s'", name, name)
		}
		return nil, "", fmt.Errorf("%w\nno safe spot yet — run the chain, check the responses, propose it with 'shrt confirm %s -note \"...\"', and a person approves it", err, name)
	}
	if !spot.DigestMatches() {
		return nil, "", fmt.Errorf("safe spot %s was changed after it was approved: its digest %s does not match its content, so it is not what a person approved.\n"+
			"Restore the file (from version control or %s), or re-approve: run the chain, check the responses, "+
			"propose it with 'shrt confirm %s -supersede -note \"...\"', and a person approves it",
			rel(e.cfg.Root, e.store.SafeSpotPath(name)), spot.Digest, rel(e.cfg.Root, filepath.Join(e.store.SafeSpotsDir, "archive")), name)
	}
	if spot.DigestKind() == store.DigestCurrent {
		return spot, "", nil
	}
	return spot, fmt.Sprintf("safe spot %s is of the older kind: its digest does not cover who approved it (confirmed_by, confirmed_at, note); "+
		"to seal the approval, propose a passing run in its place (shrt confirm %s -supersede) and have a person approve it",
		rel(e.cfg.Root, e.store.SafeSpotPath(name)), name), nil
}

func (v *verification) loadRun() error {
	e, name := v.e, v.name
	v.useRun = store.RunID(v.useRun)
	asked := v.useRun
	rec, err := e.store.LoadRun(name, v.useRun)
	if err != nil {
		return err
	}
	if rec.RunID != asked {
		v.useRun = rec.RunID
		fmt.Fprintf(os.Stderr, "verify: -run %s is run %s, the newest run record of %s (a verify replay counts)\n",
			asked, rec.RunID, name)
	}
	if v.useRun == v.spot.RunID {
		fmt.Fprintf(os.Stderr, "verify: run %s IS the safe spot's own run, so this is a control for the differ, NOT evidence about "+
			"the backend; pass a later run id, or drop -run to replay live\n", v.useRun)
	}
	if resolved, resolveErr := e.resolveChainNamed(v.arg, name); resolveErr == nil {
		v.c = resolved
	}
	v.rec = rec
	return nil
}

func (v *verification) replay(ctx context.Context) error {
	e := v.e
	c, err := e.resolveChainNamed(v.arg, v.name)
	if err != nil {
		for _, o := range doctor.OrphanSafeSpots(e.cfg) {
			if o.Name == v.name {
				return fmt.Errorf("%w\n%s; there is no chain to replay.\n%s", err, o.Line(), o.Remedy())
			}
		}
		return err
	}
	opts := runner.Options{Vars: c.CoerceVars(v.vars), Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, Build: v.build, KeepGoing: true}
	rec, err := executeChain(ctx, e, c, withLatency(opts, latencyPolicy(e), v.spot), true)
	if err != nil {
		return err
	}
	rec.ReplayOf = v.spot.RunID
	if v.save {
		if _, err := e.store.SaveRun(rec); err != nil {
			return err
		}
	}
	v.c, v.rec = c, rec
	return nil
}

func (v *verification) compare() {
	e, name, rec := v.e, v.name, v.rec
	spot, renamedSteps := diff.RenameSpotSteps(v.spot, rec.Steps)
	v.spot = spot
	v.latency = latencyFlags(e, spot, rec, latencyPolicy(e))
	report := diff.CompareMasking(spot, rec, currentVolatile(e, name))
	v.report = report
	report.DropUnsentDefaults(spot, rec, unsentDefault(e))
	report.NoteRenamedSteps(renamedSteps)
	if spotRun, err := e.store.LoadRun(name, spot.RunID); err == nil && spotRun.Redacted != nil {
		report.NoteApprovedRedact(spotRun.Redacted, rec)
		report.NoteRedactedRequests(spot, rec)
	}
	if c := v.c; c != nil {
		edited := diff.ChainChangesIn(spot, c, rec)
		report.RequestChanges = append(edited, diff.DropRefEdited(diff.CompareRequests(spot, rec, derivedRequestPath(c)), edited)...)
		report.RequestChanges = append(report.RequestChanges, diff.ExpectValueChanges(spot, rec, c, fixtureTemplate(c))...)
		report.SeparateInput(spot, rec, currentVolatile(e, name), requestFixtures(c))
	}
}

func (v *verification) sidecar() gateSidecar {
	if v.report == nil {
		return gateSidecar{}
	}
	side := earlySidecar(v.e, v.rec)
	side.Items = verifyItems(v.e, v.rec, v.report)
	if latencyPolicy(v.e).Fail {
		side.Items = append(side.Items, latencyItems(v.latency)...)
	}
	side.Sent = firstSent(v.e, v.rec, side.Items)
	side.RunToo, side.Notes, side.Latency = v.runToo, v.notes, v.latency
	if v.flaky.finding() {
		side.Flaky, side.FlakyOnly = v.flaky.rates(), v.flakyOnly
	}
	return side
}

func (v *verification) note(body *strings.Builder, line string) {
	fmt.Fprintln(body, line)
	v.notes = append(v.notes, line)
}

func (v *verification) explainInput() {
	e, c, rec, report, spotRun := v.e, v.c, v.rec, v.report, v.spot.RunID
	if len(report.RequestChanges) == 0 {
		return
	}
	only := map[string]any(v.vars)
	if v.useRun != "" {
		only = nil
	}
	fedAll := inputVars(c, report.RequestChanges)
	v.varDrift = varsDifferFromConfirmed(e, spotRun, c, rec, only, fedAll)
	defaulted := func(fed map[string]bool) []string {
		if v.useRun != "" {
			return nil
		}
		return varsConfirmedOtherwise(e, spotRun, rec, v.vars, fed)
	}
	if v.varDrift == "" {
		v.unsupplied = defaulted(fedAll)
		if len(v.unsupplied) > 0 {
			v.varDrift = varsDifferFromConfirmed(e, spotRun, c, rec, nil, fedAll)
		}
	}
	fedByVars := func(ch diff.Change) bool {
		fed := inputVars(c, []diff.Change{ch})
		return varsDifferFromConfirmed(e, spotRun, c, rec, only, fed) != "" || len(defaulted(fed)) > 0
	}
	for _, ch := range report.ChainEdits(fedByVars) {
		v.edits = append(v.edits, ch.Step+" "+ch.Path)
	}
	if v.varDrift == "" {
		return
	}
	how := "set by -var; the chain file is not what differs"
	if v.useRun != "" {
		how = "as run " + rec.RunID + " was recorded"
	}
	if len(v.unsupplied) > 0 {
		how = "this run took the chain's own value and the confirmed run ran with another, as set by -var when it was " +
			"confirmed (unless the var's default was edited since); the steps reading it are unchanged, so the chain file is not what differs"
	}
	if len(v.edits) > 0 {
		how = strings.TrimSuffix(how, "; the chain file is not what differs") +
			", and the chain file changed since it was confirmed (" + strings.Join(v.edits, ", ") + ")"
	}
	report.InputCause = fmt.Sprintf("this run's vars differ from the confirmed run's (%s), %s", v.varDrift, how)
}

func (v *verification) examine(ctx context.Context) {
	e, c, rec, report := v.e, v.c, v.rec, v.report
	v.unansweredStep, v.unansweredWhy, v.unanswered = unansweredOnly(rec, report)
	v.loss = examineSessionLoss(e, rec)
	v.life = examineTokenLifetime(e, rec)
	if v.life != nil && driftedBefore(rec, report, v.life.first.index) {
		v.runToo = v.runToo || v.life.finding()
		v.life = nil
	}
	if v.loss == nil {
		v.fresh = repeatedFreshRefusal(e, rec)
	}
	if v.fresh != nil && driftedBefore(rec, report, v.fresh.index) {
		v.runToo, v.fresh = true, nil
	}
	if v.unanswered && v.loss == nil && v.fresh == nil {
		v.dropped = repeatedUnanswered(e, rec, v.unansweredStep)
	}
	if v.loss == nil && v.fresh == nil && v.dropped == nil {
		v.flaky = detectIntermittent(e, rec)
	}
	if !report.Clean() {
		v.literal = detectLiteralCollision(e, c, rec)
		if v.literal == nil {
			v.reuse = detectFixtureReuse(e, c, rec)
		} else if driftedBefore(rec, report, v.literal.index) {
			v.runToo, v.literal = true, nil
		}
	}
	if v.literal == nil && v.reuse == nil {
		if v.idem = detectIdempotentReplay(e, c, v.spot, rec, report); v.idem != nil && driftedBefore(rec, report, v.idem.index) {
			v.idem, v.lateIdem = nil, v.idem
		}
	}
	v.driftStep, v.driftWhy, v.driftAt = firstFailureIsDrift(rec)
	v.declared = declaredDriftChanges(e, rec, report, v.driftStep)
	v.violation = v.driftStep != "" && len(v.declared) == 0 && !report.Clean() && !driftedBefore(rec, report, v.driftAt) &&
		protoViolation(ctx, e, v.driftWhy)
	v.independent = independentOfDrift(c, rec, report, v.driftStep, v.driftAt)
}

func (v *verification) notJudged(ctx context.Context) {
	name, rec, report, life, loss := v.name, v.rec, v.report, v.life, v.loss
	v.notVerdict = "this is not a verdict about the backend"
	switch {
	case life.finding():
	case life != nil && !report.Clean() && life.first.step.Status != runner.StatusPassed:
		v.headline = fmt.Sprintf("a %s at step %d %s", runner.TokenRefusalPhrase(life.first.r), life.first.step.Index, life.first.step.ID)
		v.nonBackend = exitWith(3, "could not verify %s: %s. Nothing before step %d drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend", name, life.line(), life.first.step.Index)
	case loss.finding():
	case loss != nil && !report.Clean() && !driftedBefore(rec, report, loss.index):
		v.headline = fmt.Sprintf("the backend likely restarted mid-run (a token it had accepted was refused at step %d %s)", loss.step.Index, loss.step.ID)
		v.nonBackend = exitWith(3, "could not verify %s: %s. Nothing before step %d drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend", name, loss.line(), loss.step.Index)
	case v.fresh != nil:
	case v.dropped != nil:
	case v.unanswered:
		v.unansweredHeadline()
	case v.violation:
	case v.driftStep != "" && len(v.declared) == 0 && len(v.independent) == 0 && !report.Clean() && !driftedBefore(rec, report, v.driftAt):
		v.headline = fmt.Sprintf("the response at %s does not match the descriptor", v.driftStep)
		v.nonBackend = exitWith(3, "could not verify %s: the response at %s does not match the descriptor (%s)%s; %s. "+
			"Nothing before that step drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend", name, v.driftStep, v.driftWhy, unsentWritesNote(rec, v.driftAt), driftRemedy(ctx, v.e, v.driftWhy))
	case v.idem != nil && !v.idem.literal:
		v.headline = v.idem.headline()
		v.nonBackend = exitWith(3, "could not verify %s: %s. Nothing before that step drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend. Re-run with a fresh value: shrt verify %s %s", name, v.idem.line(), name, v.idem.fresh())
	case v.reuse.finding() && !driftedBefore(rec, report, v.reuse.index):
	case v.reuse != nil && !driftedBefore(rec, report, v.reuse.index):
		v.headline = fmt.Sprintf("%s at step %s", v.reuse.verdict(), v.reuse.step)
		v.nonBackend = exitWith(3, "could not verify %s: %s. Nothing before that step drifted, and a change at or after it is not judged: "+
			"this is not a verdict about the backend. %s", name, v.reuse.line(), capitalized(v.reuse.rerun("verify", name)))
	}
}

func (v *verification) unansweredHeadline() {
	step, why, rec := v.unansweredStep, v.unansweredWhy, v.rec
	v.headline = fmt.Sprintf("step %s never got an answer", step)
	switch {
	case strings.Contains(why, transport.NoAnswerBeforeTimeout):
		v.headline = fmt.Sprintf("step %s was sent and got no answer before target.timeout", step)
	case strings.Contains(why, unavailableAnswered):
		v.headline = fmt.Sprintf("step %s was answered unavailable", step)
	case strings.HasPrefix(why, "the backend refused authentication") && refusedFreshAt(rec, step):
		v.headline = fmt.Sprintf("step %s was refused at authentication with a token a login in this run had just issued, "+
			"so the credentials work: this may be an auth regression", step)
		v.notVerdict = "re-run to confirm: a repeat at the same step is a finding"
		if st, _ := rec.Step(step); st.AuthRetry != runner.AuthRetryResent {
			v.notVerdict = "it was not re-sent, so a restart between the login and the call explains it too"
		}
	case strings.HasPrefix(why, "the backend refused authentication"):
		v.headline = fmt.Sprintf("step %s was refused at authentication", step)
	}
	v.nonBackend = couldNotVerifyAfter(v.name, step, why, rec, previousRunAttempting(v.e, rec, step))
}

func (v *verification) writeBody(ctx context.Context, body *strings.Builder) {
	name, rec, report, spot, quiet := v.name, v.rec, v.report, v.spot, v.quiet
	life, loss := v.life, v.loss
	if v.nonBackend != nil {
		fmt.Fprintf(body, "could not verify %s: %s; %s (why below)\n", name, v.headline, v.notVerdict)
	}
	if v.olderSpot != "" && !quiet {
		fmt.Fprintln(body, v.olderSpot)
	}
	if (spot.Build != "" || rec.Build != "") && !quiet {
		fmt.Fprintf(body, "safe spot build %s, this run build %s\n", orUnknown(spot.Build), orUnknown(rec.Build))
	}
	switch {
	case v.nonBackend != nil:
		if list := affectedSteps(rec, report); list != "" {
			fmt.Fprintln(body, "  affected step(s), not judged: "+list)
		}
		if life != nil && life.cachedFirstUse() {
			fmt.Fprintln(body, life.label()+life.line())
		} else if life != nil {
			v.note(body, life.label()+life.line())
		} else if loss != nil {
			v.note(body, "WARNING: "+loss.line())
		}
	case life.finding():
		v.note(body, "FINDING: "+life.line())
	case loss.finding():
		v.note(body, "FINDING: "+loss.line())
	case v.fresh != nil:
		v.note(body, "FINDING: "+v.fresh.line())
	case v.dropped != nil:
		v.note(body, "FINDING: "+v.dropped.line())
	case v.flaky.finding():
		fmt.Fprintln(body, "FINDING: "+v.flaky.line(v.verbose))
	case loss != nil:
		v.note(body, "WARNING: "+loss.line())
	}
	if life != nil && !life.finding() && !life.cachedFirstUse() && v.nonBackend == nil {
		v.note(body, life.label()+life.line())
	}
	if v.nonBackend == nil && (!v.unanswered || anyAnswered(rec)) {
		v.writeReport(ctx, body)
	}
	if quiet {
		for _, line := range warningLines(rec) {
			fmt.Fprintln(body, line)
		}
	} else if line := runner.UndeclaredFieldsLine(rec); line != "" {
		fmt.Fprintln(body, "warning: "+line)
	}
}

func (v *verification) writeReport(ctx context.Context, body *strings.Builder) {
	name, rec, report, spot := v.name, v.rec, v.report, v.spot
	driftStep, driftWhy := v.driftStep, v.driftWhy
	if !v.verbose && !v.violation && len(v.declared) == 0 && driftStep != "" {
		report.FoldSteps(unjudgedSteps(rec, v.independent, v.driftAt), fmt.Sprintf("its response, or one it reads, does not "+
			"match the descriptor (%s), so it is not judged; rebuild the descriptor (shrt catalog build) and re-run, "+
			"or add -v to list them", driftWhy))
	}
	if v.quiet {
		fmt.Fprintln(body, report.QuietText())
	} else {
		fmt.Fprintln(body, report.Text())
	}
	if line := report.FullyMaskedLine(); line != "" {
		v.notes = append(v.notes, line)
	}
	if list := report.MaskedList(); v.listMasked && list != "" {
		fmt.Fprintln(body, list)
	}
	if v.showLatency {
		fmt.Fprintln(body, diff.LatencyTable(spot.Steps, rec, latencyPolicy(v.e)))
	}
	for _, f := range v.latency {
		fmt.Fprintln(body, f.Line())
	}
	switch {
	case v.reuse.finding():
		v.note(body, "FINDING: "+v.reuse.line())
	case v.reuse != nil:
		fmt.Fprintln(body, v.reuse.line()+"; "+v.reuse.rerun("verify", name))
	}
	if v.literal != nil {
		v.note(body, "CHAIN DEFECT: "+v.literal.line())
	}
	if v.idem != nil && v.idem.literal {
		v.note(body, "CHAIN DEFECT: "+v.idem.line())
	}
	if v.lateIdem != nil {
		fmt.Fprintln(body, v.lateIdem.note())
	}
	for _, line := range v.flaky.notes() {
		fmt.Fprintln(body, "note: "+line)
	}
	if v.violation {
		v.note(body, "REGRESSION: "+violationLine(v.e, name, driftStep, driftWhy))
	}
	if !v.violation && len(v.declared) == 0 && len(v.independent) > 0 {
		fmt.Fprintf(body, "note: the response at %s does not match the descriptor (%s), so it and the steps reading it are not judged; "+
			"%d change(s) at step(s) that read nothing from it are: %s; %s\n", driftStep, driftWhy, len(v.independent),
			describeChanges(v.independent), driftRemedy(ctx, v.e, driftWhy))
	}
	if len(v.declared) > 0 {
		fmt.Fprintf(body, "note: the response at %s also does not match the descriptor (%s): the fields it does not declare were "+
			"discarded and its declared fields were compared with the safe spot's, so the change(s) above are a verdict; %s\n",
			driftStep, driftWhy, driftRemedy(ctx, v.e, driftWhy))
	}
	if added := diff.UnorderedAdded(spot, rec); len(added) > 0 {
		fmt.Fprintf(body, "chain change since the safe spot's run: %s added, not approved; it hides only a change of order, so it does not "+
			"fail verify; propose a run with it (shrt confirm %s -supersede) to have it approved\n", strings.Join(added, ", "), name)
	}
}

func (v *verification) verdict(ctx context.Context) error {
	if err := v.findingOrNoVerdict(); err != nil {
		return err
	}
	name, report, latency := v.name, v.report, v.latency
	slow := latencyFailure(name, latency, latencyPolicy(v.e))
	if v.flaky.explainsAll(report) && !v.violation && slow != nil {
		return slow
	}
	if v.flaky.explainsAll(report) && !v.violation {
		v.flakyOnly = true
		return fmt.Errorf("%s: %s", name, v.flaky.short())
	}
	if v.violation {
		return fmt.Errorf("regression: %d change(s) vs safe spot; %s", report.Counted(), violationLine(v.e, name, v.driftStep, v.driftWhy))
	}
	if err := v.inputDrift(); err != nil {
		return err
	}
	if err := v.drift(); err != nil {
		return err
	}
	if v.flaky.finding() && slow == nil {
		v.flakyOnly = true
		return fmt.Errorf("%s: %s", name, v.flaky.short())
	}
	if !v.rec.Passed() && slow == nil {
		return fmt.Errorf("chain %s: %s", v.rec.Chain, v.rec.Status)
	}
	return slow
}

func (v *verification) findingOrNoVerdict() error {
	name, rec, report := v.name, v.rec, v.report
	switch {
	case v.life.finding():
		return fmt.Errorf("%s: %s", name, v.life.line())
	case v.loss.finding() && !driftedBefore(rec, report, v.loss.index):
		return fmt.Errorf("%s: %s", name, v.loss.line())
	case v.fresh != nil:
		return fmt.Errorf("%s: %s", name, v.fresh.line())
	case v.dropped != nil:
		return fmt.Errorf("%s: %s", name, v.dropped.line())
	case v.reuse.finding() && v.nonBackend == nil && !driftedBefore(rec, report, v.reuse.index):
		return fmt.Errorf("%s: %s", name, v.reuse.line())
	case v.nonBackend != nil:
		return v.nonBackend
	case v.literal != nil:
		return fmt.Errorf("chain defect in %s: %s", name, v.literal.line())
	case v.idem != nil && v.idem.literal:
		return fmt.Errorf("chain defect in %s: %s", name, v.idem.line())
	}
	return nil
}

func (v *verification) inputDrift() error {
	name, report := v.name, v.report
	const propose = "and propose that run in place of the safe spot: shrt confirm %s -supersede -note \"...\""
	if n := len(report.Unexplained()); n > 0 && len(report.RequestChanges) > 0 {
		return fmt.Errorf("regression: %d change(s) vs safe spot are not explained by the input or chain change since it was confirmed (%d more are)",
			n, len(report.Changes)-n)
	}
	if !report.Clean() && v.varDrift != "" {
		fix := "Verify without that -var to compare like with like"
		if len(v.unsupplied) > 0 {
			fix = "Verify with " + strings.Join(v.unsupplied, " ") + " to compare like with like"
		}
		if v.useRun != "" {
			fix = "Verify a run made with the confirmed vars, or drop -run to replay the chain as it is"
		}
		if len(v.edits) > 0 {
			fix += ", and restore the chain's edit (" + strings.Join(v.edits, ", ") + ")"
		}
		return fmt.Errorf("drift with different input: %d change(s) vs safe spot, after %s because this run's vars differ from the confirmed run's (%s).\n"+
			"%s; if the new value is intended, run the chain with it until it passes,\n"+propose,
			len(report.Changes), report.InputSummary(), v.varDrift, fix, name)
	}
	if report.PrincipalChanged() {
		return fmt.Errorf("drift with different input: %d change(s) vs safe spot, and a step ran under another auth profile or principal than the confirmed run (%s).\n"+
			"Restore the step's auth and credentials; if the new principal is intended, run the chain until it passes,\n"+propose,
			len(report.Changes), principalChanges(report), name)
	}
	if !report.Clean() && report.OnlyChainChanged() {
		return fmt.Errorf("drift after a chain change: %d change(s) vs safe spot, after %s since it was confirmed: the chain changed, "+
			"not the input it sends.\nRestore the chain; if the edit is intended, run it until it passes,\n"+propose,
			len(report.Changes), report.InputSummary(), name)
	}
	if !report.Clean() && len(report.RequestChanges) > 0 {
		return fmt.Errorf("drift with different input: %d change(s) vs safe spot, after %s since it was confirmed.\n"+
			"Restore the chain's input; if the new input is intended, bring its expectations in line, run it until it passes,\n"+propose,
			len(report.Changes), report.InputSummary(), name)
	}
	return nil
}

func (v *verification) drift() error {
	name, report := v.name, v.report
	if !report.Clean() && len(report.PrincipalUnchecked) > 0 {
		return fmt.Errorf("drift, principal not checked: %d change(s) vs safe spot, which records no auth_principal, so they are a regression only if "+
			"this run logged in as the account it was confirmed with, and shrt cannot tell.\n"+
			"Check the credentials against the ones it was confirmed with; to turn principal checking on, propose a passing run in its place:\n"+
			"shrt confirm %s -supersede -note \"...\", and a person approves it", len(report.Changes), name)
	}
	if report.OnlyReordered() {
		next := ""
		if list := report.ReorderedExpectations(); len(list) > 0 {
			next = "\nThe expectation(s) reading it by position, which passed in the safe spot's run, failed: " + capList(list, 3) + "; a regression unless the order was never promised"
		}
		return fmt.Errorf("order changed: %d change(s) vs safe spot, all in list(s) holding the safe spot's items in another order (%s)%s",
			len(report.Changes), strings.Join(report.ReorderedLists(), "; "), next)
	}
	if len(v.declared) == 0 && len(v.independent) > 0 {
		return fmt.Errorf("regression: %d change(s) vs safe spot at step(s) that read nothing from %s, whose response does not match the "+
			"descriptor (%s), so it and the steps reading it are not judged: %s", len(v.independent), v.driftStep, v.driftWhy, describeChanges(v.independent))
	}
	if !report.Clean() && len(v.declared) > 0 {
		return fmt.Errorf("regression: %d change(s) vs safe spot, including %s in the declared fields of the response at %s, "+
			"which also does not match the descriptor (%s)", report.Counted(), describeChanges(v.declared), v.driftStep, v.driftWhy)
	}
	if !report.Clean() {
		first, steps := firstChange(report, v.rec)
		at := ""
		if first != nil {
			at = fmt.Sprintf(" at %d step(s), first %s %s", steps, first.Step, first.Path)
		}
		return fmt.Errorf("regression: %d change(s) vs safe spot%s%s\n%s", report.Counted(), at, regressionShape(report), capitalized(intendedChangeNext(name)))
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
	fixture := fixtureTemplate(c)
	return func(step, path string) bool {
		v, ok := requestTemplate(c, step, path)
		text, isText := v.(string)
		return ok && isText && fixture(text)
	}
}

func fixtureTemplate(c *chain.Chain) func(string) bool {
	isolating := isolationVars(c)
	return func(text string) bool {
		refs := requestRef.FindAllStringSubmatch(text, -1)
		if len(refs) == 0 || !namedAround(text) {
			return false
		}
		fed := false
		for _, m := range refs {
			n := varName.FindStringSubmatch(strings.TrimSpace(m[1]))
			if n == nil {
				return false
			}
			if _, declared := c.Vars[n[1]]; !isolating[n[1]] && (!declared || n[1] == "tag") {
				return false
			}
			fed = fed || isolating[n[1]]
		}
		return fed
	}
}

func namedAround(text string) bool {
	rest := strings.TrimSpace(requestRef.ReplaceAllString(text, ""))
	return strings.Trim(rest, "0123456789.+-") != "" || len(requestRef.FindAllString(text, 2)) > 1
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

func verifyVerdict(e *env, name string, rec *runner.Record, report *diff.Report, flaky *intermittentFailure, noVerdict bool, err error, body string) (string, string) {
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
	why = strings.TrimPrefix(why, name+": ")
	if kind, _, ok := strings.Cut(why, ":"); ok && len(kind) < 40 && !strings.Contains(kind, name) {
		why = kind
	}
	first, steps := firstChange(report, rec)
	if first == nil {
		return fmt.Sprintf("%s: FAILED vs safe spot %s: %s\n", name, report.SafeSpotID, capText(why, 200)), body
	}
	a := verifyAttribution(e, rec, report)
	if why == "regression" || why == "order changed" {
		class := flaky.classOf(report, a)
		why = class(*first) + alsoClasses(report, first, class)
	}
	line := fmt.Sprintf("%s: DRIFT (%s), %d step(s) changed vs safe spot %s; first: %s", name, why, steps, report.SafeSpotID, changeAt(rec, *first))
	r := a.of(first.Step, first.Path)
	if s := r.String(); s != "" {
		line += "; " + s
	}
	if req := requestLine(r, first.Step, recordSent(e, rec)); req != "" && strings.HasPrefix(why, "regression") {
		line += "\n  " + req
	}
	if hint := tellApart(e, r, first.Path); hint != "" {
		line += "\n  " + hint
	}
	return line + "\n" + otherRoots(e, rec, report, first), body
}

func otherRoots(e *env, rec *runner.Record, report *diff.Report, first *diff.Change) string {
	items := verifyItems(e, rec, report)
	seen := map[string]bool{}
	for _, it := range items {
		if it.Step == first.Step && (it.Path == first.Path || first.Kind == diff.KindStatus) {
			seen[it.root()], seen[it.Step+" "+it.Reason.String()] = true, it.Reason.Kind != ""
		}
	}
	if len(seen) == 0 {
		return ""
	}
	out := ""
	for _, it := range items {
		r, same := it.root(), it.Step+" "+it.Reason.String()
		if seen[r] || it.Reason.Kind != "" && seen[same] {
			seen[r] = true
			continue
		}
		seen[r], seen[same] = true, true
		out += fmt.Sprintf("  also: %s (%s) %s", it.Step, shortRPC(it.Call), it.headline())
		if s := it.Reason.String(); s != "" {
			out += "; " + s
		}
		out += "\n"
	}
	return out
}

func alsoClasses(report *diff.Report, first *diff.Change, classOf func(diff.Change) string) string {
	class := classOf(*first)
	byStep := map[string]string{}
	for _, status := range []bool{false, true} {
		for _, c := range report.Changes {
			if _, seen := byStep[c.Step]; seen || c.Kind == diff.KindNotReached || c.Step == first.Step || (c.Kind == diff.KindStatus) != status {
				continue
			}
			byStep[c.Step] = classOf(c)
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

func firstChange(report *diff.Report, rec *runner.Record) (*diff.Change, int) {
	var first *diff.Change
	steps := map[string]bool{}
	failing := func(step string) bool {
		if rec == nil {
			return false
		}
		st, ok := rec.Step(step)
		return ok && st != nil && (st.Status == runner.StatusFailed || st.Status == runner.StatusError)
	}
	best := 0
	for i, c := range report.Changes {
		if c.Kind == diff.KindNotReached {
			continue
		}
		steps[c.Step] = true
		level := 0
		if failing(c.Step) {
			level = 1
			if report.Class(c) != "order changed" {
				level = 2
			}
		}
		switch {
		case first == nil || level > best:
			first, best = &report.Changes[i], level
		case c.Step == first.Step && rank(c) > rank(*first):
			first = &report.Changes[i]
		}
	}
	if first != nil && best > 0 && rec != nil {
		if root := rootChange(report, rec, *first); root != nil {
			first = root
		}
	}
	return first, len(steps)
}

func rootChange(report *diff.Report, rec *runner.Record, c diff.Change) *diff.Change {
	bad := map[string]bool{}
	for _, x := range report.Changes {
		if x.Kind != diff.KindNotReached {
			bad[x.Step] = true
		}
	}
	w, fallback := suspectWrite(rec, c.Step, c.Path, bad, -1)
	if fallback || w < 0 || !bad[rec.Steps[w].ID] {
		return nil
	}
	var root *diff.Change
	for i, x := range report.Changes {
		if x.Step == rec.Steps[w].ID && x.Kind != diff.KindNotReached && (root == nil || rank(x) > rank(*root)) {
			root = &report.Changes[i]
		}
	}
	return root
}

func rank(c diff.Change) int {
	switch {
	case c.Kind == diff.KindStatus && (c.Want == runner.StatusError || c.Got == runner.StatusError):
		return 2
	case c.Kind == diff.KindStatus:
		return 0
	}
	return 1
}

func changeAt(rec *runner.Record, c diff.Change) string {
	rpc := ""
	if st, ok := rec.Step(c.Step); ok && st != nil {
		rpc = " (" + shortRPC(st.Call) + ")"
		for _, ex := range st.Expect {
			if c.Kind == diff.KindStatus && !ex.Passed && ex.Rule != "unevaluated" {
				want, got := gatePair(ex.Want, ex.Got)
				return fmt.Sprintf("%s%s %s %s", c.Step, rpc, ex.Path, chain.WantGot(ex.Rule, want, got))
			}
		}
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
		if st.ID != step || !runner.RefusedFreshToken(st) {
			continue
		}
		tail := "re-run verify to confirm: a repeat at the same step is reported as a finding"
		if st.AuthRetry != runner.AuthRetryResent {
			tail = "it was not re-sent (not a read), so a restart between the login and this call explains it as well, and a repeat " +
				"is not a finding; re-run verify"
		}
		return exitWith(3, "could not verify %s: step %q was refused at authentication (%s) with a token a login in this run had just "+
			"issued, so the credentials work: this may be an auth regression in the backend. Nothing before it drifted, and %s. "+
			"The step record says what was tried (auth_retry, error); %s", name, step, why, past, tail)
	}
	remedy, got := unansweredRemedy(why), "never got an answer"
	for _, st := range rec.Steps {
		if st.ID == step && runner.NotAnsweredByService(st) {
			remedy, got = "wait until it is up (as after a restart) and run verify again", "was answered unavailable"
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
	return exitWith(3, "could not verify %s: step %q %s (%s); nothing before it drifted, and %s. "+
		"This is not a verdict about the backend: %s", name, step, got, why, past, remedy)
}

const unavailableAnswered = "the backend or a gateway in front of it answered unavailable"

var gatewayLogin = regexp.MustCompile(`(?i)login rejected: (http_50[234]|unavailable)\b`)

func unansweredRemedy(why string) string {
	lower := strings.ToLower(why)
	switch {
	case gatewayLogin.MatchString(why):
		return "the login was answered unavailable, by the backend or a gateway in front of it (as during a restart), so wait until it is up " +
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
				why = fmt.Sprintf("HTTP %d %s: %s", st.HTTPStatus, why, unavailableAnswered)
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
