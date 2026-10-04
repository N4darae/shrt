package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func init() {
	register(&command{name: "run", summary: "replay a chain against the target and record the result", run: runRun})
}

type varFlags map[string]any

func (v varFlags) String() string { return "" }

func (v varFlags) Set(s string) error {
	k, val, ok := strings.Cut(s, "=")
	if !ok {
		return fmt.Errorf("expected key=value, got %q", s)
	}
	v[k] = typedVar(val)
	return nil
}

func typedVar(s string) any {
	switch s {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && strconv.FormatInt(n, 10) == s {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && strconv.FormatFloat(f, 'f', -1, 64) == s {
		return f
	}
	return s
}

func runRun(ctx context.Context, args []string) (err error) {
	side := func() gateSidecar { return gateSidecar{} }
	defer func() { writeGateSidecar(side(), err) }()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	vars := varFlags{}
	fs.Var(vars, "var", "set a chain var as `key=value`, repeatable")
	save := fs.Bool("save", true, "persist the run record")
	dry := fs.Bool("dry-run", false, "resolve and validate every request, send nothing")
	asJSON := fs.Bool("json", false, "print the run record as JSON")
	quiet := fs.Bool("quiet", false, "no per-step progress; a green chain prints its verdict line only")
	build := fs.String("build", "", buildFlagUsage)
	keepGoing := fs.Bool("keep-going", false, "run past a failed step; a step reading a failed step is recorded skipped")
	verbose := fs.Bool("v", false, "with -keep-going, print every step, not only the ones that did not pass")
	setUsage(fs, "usage: shrt run <chain> [flags]", runExitCodes)
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: shrt run <chain> [flags]")
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	c, err := e.resolveChain(rest[0])
	if err != nil {
		return err
	}
	if err := refuseShadowingChainFile(e, rest[0], c); err != nil {
		return err
	}

	supplied := c.CoerceVars(vars)
	if err := checkUnusedVars(c, vars, supplied); err != nil {
		return err
	}

	var spot *store.SafeSpot
	if !*dry {
		if loaded, err := e.store.LoadSafeSpot(c.Name); err == nil && loaded.DigestMatches() {
			spot = loaded
		}
	}
	var pinnedRef *runner.Record
	latencySpot := spot
	if !*dry && len(c.KeptRed) > 0 {
		pinnedRef = pinnedReference(e, c, &runner.Record{Chain: c.Name, ChainDigest: c.Digest(), Target: e.cfg.Target.BaseURL, StartedAt: time.Now()})
		if spot == nil && pinnedRef != nil {
			latencySpot = &store.SafeSpot{Chain: c.Name, RunID: pinnedRef.RunID, Steps: pinnedRef.Steps}
		}
	}
	rec, err := executeChain(ctx, e, c, withLatency(runner.Options{
		Vars: supplied, Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, DryRun: *dry, KeepGoing: *keepGoing, Build: *build,
	}, latencyPolicy(e), latencySpot), *quiet || *asJSON, !*keepGoing || *verbose)
	if err != nil {
		return err
	}
	var flaky *intermittentFailure
	var drift []diff.Change
	var driftReport *diff.RunReport
	flakyOnly := false
	var notes []string
	var pinnedSlow, slow []diff.LatencyFlag
	side = func() gateSidecar {
		s := runSidecar(e, c, rec, drift, driftReport, pinnedRef)
		if flaky.finding() {
			s.Flaky, s.FlakyOnly = flaky.rates(), flakyOnly
		}
		s.Notes, s.Latency = notes, slow
		return s
	}
	if !*dry && len(c.KeptRed) > 0 {
		drift, driftReport = judgePinnedDrift(e, c, rec, pinnedRef)
		if spot == nil && latencySpot != nil {
			for _, f := range latencyFlags(e, latencySpot, rec, latencyPolicy(e)) {
				f.Against = "run " + pinnedRef.RunID + " (the last run that failed as pinned)"
				pinnedSlow = append(pinnedSlow, f)
				rec.KeptRedSlow = append(rec.KeptRedSlow, f.Step)
			}
		}
	}
	savedPath := ""
	if *save && !*dry {
		path, err := e.store.SaveRun(rec)
		if err != nil {
			return err
		}
		savedPath = path
		if !*asJSON && !*quiet {
			fmt.Printf("\nrun %s -> %s\n", rec.RunID, path)
		}
	}
	if *asJSON {
		if err := emitJSON(rec); err != nil {
			return err
		}
		if literal := detectLiteralCollision(e, c, rec); literal != nil && !rec.Passed() && rec.KeptRed != runner.KeptRedAsPinned {
			return fmt.Errorf("chain defect in %s: %s", c.Name, literal.line())
		}
		return runVerdict(rec)
	}
	lead := ""
	if !rec.Passed() && rec.KeptRed != runner.KeptRedAsPinned {
		if literal := detectLiteralCollision(e, c, rec); literal != nil {
			lead = "CHAIN DEFECT: " + literal.line()
			notes = append(notes, lead)
		} else if reuse := detectFixtureReuse(e, c, rec); reuse.finding() {
			lead = "FINDING: " + reuse.line()
			notes = append(notes, lead)
		} else if reuse != nil {
			lead = reuse.line() + "; " + reuse.rerun("run", rest[0])
		}
	}
	if lead != "" && rec.KeptRed == runner.KeptRedNotAsPinned {
		rec.KeptRedNote = keptRedNotJudged(rec.KeptRedNote)
	}
	var life *tokenLifetime
	var loss *sessionLoss
	var fresh *freshRefusal
	if !*dry {
		life, loss = examineTokenLifetime(e, rec), examineSessionLoss(e, rec)
		if loss == nil {
			fresh = repeatedFreshRefusal(e, rec)
		}
		if loss == nil && fresh == nil {
			if f := detectIntermittent(e, rec); f != nil && (!rec.Passed() || f.finding()) {
				flaky = f
			}
		}
	}
	finding := rec.KeptRed == "" && (life.finding() || loss.finding() || fresh != nil || rec.Passed() && flaky != nil && flaky.finding())
	if *quiet {
		fmt.Println(runSummary(e, quietRecord(rec), *dry, false, lead, finding))
		if savedPath != "" && !quietlyGreen(rec) {
			fmt.Printf("  run %s -> %s\n", rec.RunID, savedPath)
		}
	} else {
		fmt.Println(runSummary(e, rec, *dry, true, lead, finding))
	}
	if line := neverRanLine(c, rec); line != "" && !*dry {
		fmt.Println("  " + line)
	}
	if spot != nil {
		renamed, _ := diff.RenameSpotSteps(spot, rec.Steps)
		slow = latencyFlags(e, renamed, rec, latencyPolicy(e))
	}
	slow = append(slow, pinnedSlow...)
	for _, f := range slow {
		fmt.Println("  " + f.Line())
	}
	if step := timedOutStep(rec); step != "" {
		fmt.Printf("  step %q: %s, and run it again\n", step, timeoutRemedy)
	}
	if life != nil && !life.cachedFirstUse() {
		notes = append(notes, life.label()+life.line())
		fmt.Println("  " + life.label() + life.line())
		if life.finding() && rec.KeptRed == "" {
			return fmt.Errorf("chain %s: %s", rec.Chain, life.line())
		}
	}
	if loss != nil {
		fmt.Println("  " + loss.line())
		if loss.finding() && rec.KeptRed == "" {
			return fmt.Errorf("chain %s: %s", rec.Chain, loss.line())
		}
	} else if fresh != nil {
		fmt.Println("  " + fresh.line())
		if rec.KeptRed == "" {
			return fmt.Errorf("chain %s: %s", rec.Chain, fresh.line())
		}
	} else if flaky != nil {
		for _, line := range flaky.notes() {
			fmt.Println("  note: " + line)
		}
		if flaky.finding() {
			fmt.Println("  FINDING: " + flaky.line(true))
			if others := flaky.otherFailures(e, rec); len(others) > 0 && rec.KeptRed == "" {
				return fmt.Errorf("chain %s: failed at %s, not an intermittent failure; also %s", rec.Chain, strings.Join(others, ", "), flaky.short())
			}
			flakyOnly = rec.KeptRed == "" || rec.KeptRed == runner.KeptRedAsPinned
			return fmt.Errorf("chain %s: %s", rec.Chain, flaky.short())
		}
	}
	if line := pinItLine(sliceChainRef(rest[0], c), c, rec); line != "" && !*dry && lead == "" && flaky == nil && life == nil && loss == nil {
		fmt.Println(line)
	}
	if err := runVerdict(rec); err != nil {
		if rec.KeptRed == "" {
			return shownError{err}
		}
		return err
	}
	if rec.KeptRed == runner.KeptRedAsPinned {
		return keptRedLatencyFailure(rec.Chain, pinnedSlow, latencyPolicy(e))
	}
	return nil
}

const runExitCodes = "\nexit codes:\n" +
	"  0  passed; a kept_red chain failed exactly as pinned; a -dry-run resolved every request\n" +
	"  1  failed: an expectation, a FINDING, kept_red not as pinned or gone, or refused before sending\n" +
	"  3  no verdict: unreachable, answered unavailable, a restart mid-run, login or auth refused; re-run\n"

func runVerdict(rec *runner.Record) error {
	switch rec.KeptRed {
	case runner.KeptRedAsPinned:
		return nil
	case runner.KeptRedNotAsPinned:
		if rec.KeptRedNew != "" {
			return fmt.Errorf("chain %s: kept red, but it did not fail as pinned: %s", rec.Chain, shortNewFailure(rec.KeptRedNew))
		}
		if head, rest, ok := strings.Cut(rec.KeptRedNote, ":\n"); ok {
			first, _, _ := strings.Cut(rest, "\n")
			if strings.Contains(head, "now return something else") {
				first = "a pinned step now returns something else: " + first
			}
			return fmt.Errorf("chain %s: kept red, but it did not fail as pinned: %s", rec.Chain, first)
		}
		if _, one, ok := strings.Cut(rec.KeptRedNote, ", but "); ok {
			return fmt.Errorf("chain %s: kept red, but it did not fail as pinned: %s", rec.Chain, one)
		}
		return fmt.Errorf("chain %s: kept red, but it did not fail as pinned", rec.Chain)
	case runner.KeptRedGone:
		return fmt.Errorf("chain %s: kept red, but it passed: the pinned defect is gone", rec.Chain)
	}
	switch rec.Status {
	case runner.StatusPassed:
		return nil
	case runner.StatusError:
		return exitWith(3, "chain %s: %s", rec.Chain, rec.Status)
	}
	return fmt.Errorf("chain %s: %s", rec.Chain, rec.Status)
}

func newFailureLine(rec *runner.Record, stepsShown bool) string {
	if !stepsShown {
		return rec.KeptRedNew
	}
	ids := map[string]bool{}
	for _, st := range rec.Steps {
		if st != nil {
			ids[st.ID] = true
		}
	}
	named, seen := []string{}, map[string]bool{}
	kinds := map[string][]string{}
	for _, item := range strings.Split(strings.TrimPrefix(rec.KeptRedNew, runner.NewFailurePrefix), "; ") {
		id, _, _ := strings.Cut(item, " ")
		if !ids[id] {
			continue
		}
		if !seen[id] {
			seen[id] = true
			named = append(named, id)
		}
		if kind := runner.ListChangeKind(item); kind != "" && !slices.Contains(kinds[id], kind) {
			kinds[id] = append(kinds[id], kind)
		}
	}
	if len(named) == 0 {
		return rec.KeptRedNew
	}
	for i, id := range named {
		if len(kinds[id]) > 0 {
			named[i] = id + " (" + strings.Join(kinds[id], ", ") + ")"
		}
	}
	return runner.NewFailurePrefix + strings.Join(named, ", ") + " (each failure is on its step's line above)"
}

func shortNewFailure(line string) string {
	found := strings.Split(strings.TrimPrefix(line, runner.NewFailurePrefix), "; ")
	if len(found) == 1 && len(line) <= 200 {
		return line
	}
	first := found[0]
	if len(first) > 160 {
		first = first[:157] + "..."
	}
	if len(found) > 1 {
		return fmt.Sprintf("%s%s, and %d more (listed above)", runner.NewFailurePrefix, first, len(found)-1)
	}
	return runner.NewFailurePrefix + first
}

func checkUnusedVars(c *chain.Chain, vars map[string]any, supplied map[string]any) error {
	unused := c.UnusedVarNames(vars)
	if len(unused) == 0 {
		return nil
	}
	reads := c.DeclaredVarNames()
	typos, ignored := []string{}, []string{}
	for _, name := range unused {
		if len(namecase.Closest(name, reads, 1)) > 0 {
			typos = append(typos, name)
		} else {
			ignored = append(ignored, name)
		}
	}
	if len(typos) > 0 {
		return unusedVarError(typos, c.Name, reads)
	}
	for _, name := range ignored {
		delete(supplied, name)
	}
	if len(reads) == 0 {
		return nil
	}
	fmt.Fprintf(os.Stderr, "warning: -var %s: chain %q never reads %s (it reads %s)\n",
		strings.Join(ignored, ", "), c.Name, pluralWord(len(ignored), "it", "them"), strings.Join(reads, ", "))
	return nil
}

func unusedVarError(unused []string, chainName string, reads []string) error {
	readsLine := "this chain reads no vars at all, so any -var is rejected"
	if len(reads) > 0 {
		readsLine = "vars this chain reads: " + strings.Join(reads, ", ")
	}
	return fmt.Errorf("-var %s names a variable chain %q never reads, so it would have no effect, and it is close "+
		"to a var the chain does read (%s), so it looks mistyped.\n"+
		"A chain isolates its fixtures with vars, so a mistyped one silently collapses every run onto "+
		"the same key. Check the spelling, or drop the flag.\n%s",
		strings.Join(unused, ", "), chainName, strings.Join(closestReads(unused, reads), ", "), readsLine)
}

const buildFlagUsage = "stamp this build `id` into the run record (overrides target.build_header)"

func executeChain(ctx context.Context, e *env, c *chain.Chain, opts runner.Options, quiet bool, everyStep ...bool) (*runner.Record, error) {
	r, _, err := runner.NewFromConfig(ctx, e.cfg, e.cat)
	if err != nil {
		return nil, err
	}
	condensed := len(everyStep) > 0 && !everyStep[0]
	passed, behind, behindOrder, answered := 0, map[string]int{}, []string{}, map[string][]string{}
	if !quiet {
		idWidth := longestStepID(c)
		unreachableShown := false
		skips := runner.NewSkipCondenser()
		warned := map[string]string{}
		r.OnStep = func(sr *runner.StepRecord) {
			if sr.NotSentUnreachable() {
				if !unreachableShown {
					unreachableShown = true
					fmt.Printf("%-5s %2d.. every remaining step %s\n", statusMark(sr.Status, opts.DryRun), sr.Index, sr.Error)
				}
				return
			}
			if condensed && sr.Status == runner.StatusPassed && len(shownWarnings(sr)) == 0 {
				passed++
				return
			}
			if src := unevaluatedBehind(sr); condensed && src != "" {
				if behind[src] == 0 {
					behindOrder = append(behindOrder, src)
				}
				behind[src]++
				if line := answeredHeld(sr); line != "" {
					answered[src] = append(answered[src], line)
				}
				return
			}
			fmt.Println(progressLine(sr, opts.DryRun, idWidth))
			shownDetail := ""
			for _, ex := range sr.Expect {
				if ex.Passed {
					continue
				}
				if ex.Detail != "" && ex.Detail == shownDetail {
					ex.Detail = "the same as above"
				} else {
					shownDetail = ex.Detail
				}
				fmt.Printf("       %s\n", ex.String())
			}
			if sr.Error != "" {
				fmt.Printf("       %s\n", skips.Condense(sr.ID, sr.Error))
			}
			for _, line := range shownWarnings(sr) {
				if at, seen := warned[line]; seen {
					fmt.Printf("       the same warning as at step %s above\n", at)
					continue
				}
				warned[line] = sr.ID
				fmt.Printf("       %s\n", line)
			}
		}
	}
	rec, err := r.Run(ctx, c, opts)
	if err != nil || quiet || !condensed {
		return rec, err
	}
	for _, src := range behindOrder {
		line := fmt.Sprintf("%d step(s) unevaluated behind %s", behind[src], src)
		if len(answered[src]) > 0 {
			line += ": " + capList(answered[src], 3)
		}
		fmt.Println(line)
	}
	fmt.Printf("%d step(s) passed (-v prints every step)\n", passed)
	return rec, nil
}

func answeredHeld(sr *runner.StepRecord) string {
	var body any
	if sr.Status != runner.StatusFailed || json.Unmarshal(sr.Response, &body) != nil {
		return ""
	}
	var vals []string
	for _, ex := range sr.Expect {
		if ex.Rule != "unevaluated" {
			continue
		}
		if v, ok := chain.Get(body, ex.Path); ok {
			vals = append(vals, ex.Path+"="+capText(compactValue(v), 60))
		}
	}
	if len(vals) == 0 {
		return ""
	}
	return sr.ID + " answered " + strings.Join(vals, ", ") + " (not judged)"
}

func unevaluatedBehind(sr *runner.StepRecord) string {
	if sr.Status == runner.StatusSkipped && strings.HasPrefix(sr.Error, "not sent: ${") {
		_, rest, ok := strings.Cut(sr.Error, `reads step "`)
		if !ok {
			return ""
		}
		src, _, _ := strings.Cut(rest, `"`)
		return src
	}
	if sr.Status != runner.StatusFailed || sr.Error != "" {
		return ""
	}
	src, _ := heldBackBy(sr)
	return src
}

func progressLine(sr *runner.StepRecord, dry bool, idWidth ...int) string {
	w := 24
	if len(idWidth) > 0 && idWidth[0] > w {
		w = idWidth[0]
	}
	line := fmt.Sprintf("%-5s %2d %-*s %-52s %4dms", statusMark(sr.Status, dry), sr.Index, w, sr.ID, sr.Call, sr.LatencyMS)
	if sr.WaitedMS > 0 {
		line += fmt.Sprintf("  (sent after waiting %s)", (time.Duration(sr.WaitedMS) * time.Millisecond).Round(time.Millisecond))
	}
	return line
}

func longestStepID(c *chain.Chain) int {
	n := 0
	for _, s := range c.Steps {
		if s != nil && len(s.ID) > n {
			n = len(s.ID)
		}
	}
	return n
}

func statusMark(s string, dry bool) string {
	switch s {
	case runner.StatusPassed:
		return "ok"
	case runner.StatusFailed:
		return "FAIL"
	case runner.StatusError:
		return "ERROR"
	case runner.StatusSkipped:
		if dry {
			return "--"
		}
		return "SKIP"
	default:
		return strings.ToUpper(s)
	}
}

func pinItLine(ref string, c *chain.Chain, rec *runner.Record) string {
	if rec.Status != runner.StatusFailed || rec.KeptRed != "" || len(c.KeptRed) > 0 {
		return ""
	}
	if len(expectationFailures(rec)) == 0 {
		return ""
	}
	return "pin it: shrt chain pin " + ref + " (re-runs with -keep-going when needed)"
}

func neverRanLine(c *chain.Chain, rec *runner.Record) string {
	if c == nil || rec.KeepGoing || rec.KeptRed != "" || rec.Status != runner.StatusFailed {
		return ""
	}
	ran := map[string]bool{}
	for _, sr := range rec.Steps {
		if sr != nil {
			ran[sr.ID] = true
		}
	}
	left := []string{}
	for _, s := range c.Steps {
		if s != nil && !ran[s.ID] {
			left = append(left, s.ID)
		}
	}
	if len(left) == 0 {
		return ""
	}
	return fmt.Sprintf("%d later step(s) were not run (%s): this failure may not be the only one; run with -keep-going to see them",
		len(left), capList(left, 3))
}

func runSummary(e *env, rec *runner.Record, dry, stepsShown bool, lead string, finding bool) string {
	var b strings.Builder
	verdict := strings.ToUpper(rec.Status)
	if finding && rec.Passed() {
		verdict = "FINDING (every step passed)"
	}
	if dry && rec.Passed() {
		verdict = "DRY-RUN OK (resolved and validated, nothing sent)"
	}
	if rec.KeptRed == runner.KeptRedAsPinned {
		verdict = "FAILED AS PINNED (kept red)"
	}
	if rec.KeptRed == runner.KeptRedNotAsPinned {
		verdict = "FAILED, NOT AS PINNED (kept red)"
	}
	if rec.KeptRed == runner.KeptRedGone {
		verdict = "PINNED DEFECT GONE (kept red, every step passed; exit 1 until kept_red is removed)"
	}
	fmt.Fprintf(&b, "%s: %s in %dms", rec.Chain, verdict, rec.DurationMS)
	if rec.KeptRedNew != "" {
		fmt.Fprintf(&b, "\n  %s", newFailureLine(rec, stepsShown))
	}
	if lead != "" {
		fmt.Fprintf(&b, "\n  %s", lead)
	}
	if rec.Build != "" {
		fmt.Fprintf(&b, "\n  build: %s at %s", rec.Build, rec.Target)
	}
	if len(rec.FailedSteps) > 0 {
		fmt.Fprintf(&b, "\n  did not pass: %s", capList(rec.FailedSteps, 10))
	}
	if failure := rec.Failure; failure != "" {
		if stepsShown && rec.KeptRed != "" {
			failure, _, _ = strings.Cut(failure, "\n")
			failure += " (each step's failure is on its line above)"
		} else if stepsShown && rec.KeepGoing && strings.HasPrefix(failure, "-keep-going: ") {
			failure, _, _ = strings.Cut(failure, "\n")
			failure += " (above)"
		}
		fmt.Fprintf(&b, "\n  %s", strings.ReplaceAll(failure, "\n", "\n  "))
	}
	for _, line := range failureRequests(e, rec, dry) {
		fmt.Fprintf(&b, "\n  %s", line)
	}
	if rec.KeptRedNote != "" {
		fmt.Fprintf(&b, "\n  kept red (%s): %s", rec.KeptRed, strings.ReplaceAll(rec.KeptRedNote, "\n", "\n    "))
	}
	for _, line := range warningLines(rec) {
		fmt.Fprintf(&b, "\n  %s", line)
	}
	if len(rec.Exports) > 0 {
		names := sortedKeys(rec.Exports)
		if dry {
			fmt.Fprintf(&b, "\n  exports not produced in a dry run (nothing was sent, so no response exists to read them from): %s",
				strings.Join(names, ", "))
			return b.String()
		}
		b.WriteString("\n  exports:")
		for _, k := range names {
			fmt.Fprintf(&b, " %s=%s", k, exportJSON(rec.Exports[k]))
		}
	}
	return b.String()
}

func failureRequests(e *env, rec *runner.Record, dry bool) []string {
	if dry || rec.KeptRed != "" || rec.Status != runner.StatusFailed {
		return nil
	}
	att := runAttribution(e, rec)
	type failure struct {
		st   *runner.StepRecord
		r    reason
		path string
	}
	failures, writes := []failure{}, map[string]bool{}
	for _, st := range rec.Steps {
		if st == nil || st.Status == runner.StatusPassed || st.Status == runner.StatusSkipped {
			continue
		}
		path := failedPath(st)
		r := att.of(st.ID, path)
		if w := r.blamed(st.ID); w != "" {
			writes[w] = true
		}
		failures = append(failures, failure{st, r, path})
	}
	lines, hints, count, order, named := map[string]string{}, map[string]string{}, map[string]int{}, []string{}, map[string]bool{}
	for _, f := range failures {
		key, w := "", f.r.blamed(f.st.ID)
		switch {
		case w != "":
			key = w
		case f.r.Kind != "" && !f.r.blames():
			key = f.st.Call
		case writes[f.st.ID]:
			key = f.st.ID
		}
		if _, seen := count[key]; !seen {
			order = append(order, key)
		}
		if lines[key] == "" || !named[key] && f.r.Kind != "" {
			lines[key], named[key], hints[key] = suspectLine(f.r, f.st.ID, recordSent(e, rec)), f.r.Kind != "", tellApart(e, f.r, f.path)
		}
		count[key]++
	}
	sort.SliceStable(order, func(i, j int) bool { return count[order[i]] > count[order[j]] })
	out, hint := []string{}, ""
	for _, key := range order {
		if lines[key] != "" && len(out) < 3 {
			out = append(out, lines[key])
			hint = cmp.Or(hint, hints[key])
		}
	}
	if hint != "" {
		out = append(out, hint)
	}
	return out
}

func quietlyGreen(rec *runner.Record) bool {
	return rec.Passed() || rec.KeptRed == runner.KeptRedAsPinned
}

func quietRecord(rec *runner.Record) *runner.Record {
	if !quietlyGreen(rec) {
		return rec
	}
	c := *rec
	c.Exports = nil
	c.Build = ""
	if rec.KeptRed == runner.KeptRedAsPinned {
		c.FailedSteps = nil
		c.Failure = ""
		c.KeptRedNote = ""
	}
	return &c
}

func warningLines(rec *runner.Record) []string {
	out := []string{}
	if rec.Warning != "" {
		out = append(out, "warning: "+rec.Warning)
	}
	warnings, stepsOf := []string{}, map[string][]string{}
	for _, sr := range rec.Steps {
		if sr == nil {
			continue
		}
		for _, line := range shownWarnings(sr) {
			if _, seen := stepsOf[line]; !seen {
				warnings = append(warnings, line)
			}
			if ids := stepsOf[line]; len(ids) == 0 || ids[len(ids)-1] != sr.ID {
				stepsOf[line] = append(ids, sr.ID)
			}
		}
	}
	for _, line := range warnings {
		out = append(out, fmt.Sprintf("warning [%s]: %s", capList(stepsOf[line], 10), line))
	}
	if line := runner.UndeclaredFieldsLine(rec); line != "" {
		out = append(out, "warning: "+line)
	}
	return out
}

func shownWarnings(sr *runner.StepRecord) []string {
	cachedFirstUse := sr.AuthRetry == runner.AuthRetryResent && len(sr.TokenRefused) > 0
	for _, r := range sr.TokenRefused {
		cachedFirstUse = cachedFirstUse && r.Cached && r.FirstUse
	}
	out := []string{}
	for _, line := range strings.Split(sr.Warning, "\n") {
		if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, runner.UndeclaredFieldsWarning) {
			continue
		}
		if cachedFirstUse && (line == runner.CachedTokenResent || strings.HasPrefix(line, "the token was refused")) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func capList(items []string, max int) string {
	if len(items) <= max {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:max], ", "), len(items)-max)
}

func exportJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimRight(buf.String(), "\n")
}

func refuseShadowingChainFile(e *env, ref string, c *chain.Chain) error {
	if !strings.ContainsAny(ref, "/\\") && !strings.HasSuffix(ref, ".yaml") && !strings.HasSuffix(ref, ".yml") {
		return nil
	}
	for _, ext := range []string{".yaml", ".yml"} {
		own := filepath.Join(e.chainsDir(), c.Name+ext)
		ownInfo, err := os.Stat(own)
		if err != nil {
			continue
		}
		if given, err := os.Stat(ref); err == nil && os.SameFile(given, ownInfo) {
			return nil
		}
		return fmt.Errorf("%s is named %q, the name of the chain %s in paths.chains, so its runs would be stored as that "+
			"chain's runs, proposed by shrt confirm %s and counted by shrt chain hollow as that chain. Nothing was sent: "+
			"rename it (name: %s-scratch, say), or run the chain itself: shrt run %s",
			ref, c.Name, rel(e.cfg.Root, own), c.Name, c.Name, c.Name)
	}
	return nil
}

func keptRedNotJudged(note string) string {
	pins, _, found := strings.Cut(note, ", but ")
	if !found {
		pins = "kept_red"
	}
	return "not judged: " + pins + ", and the run collided with data an earlier run or another client left (above), " +
		"so the steps that failed or were not sent say nothing about the pinned defect; re-run with a fresh value"
}

func closestReads(unused, reads []string) []string {
	out := []string{}
	for _, name := range unused {
		for _, c := range namecase.Closest(name, reads, 1) {
			if !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
	}
	return out
}

func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
