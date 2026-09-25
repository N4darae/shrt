package main

import (
	"bytes"
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

func runRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	vars := varFlags{}
	fs.Var(vars, "var", "set a chain var as `key=value`, repeatable")
	save := fs.Bool("save", true, "persist the run record")
	dry := fs.Bool("dry-run", false, "resolve and validate every request, send nothing")
	asJSON := fs.Bool("json", false, "print the run record as JSON")
	quiet := fs.Bool("quiet", false, "no per-step progress; a green chain prints its verdict line only")
	build := fs.String("build", "", buildFlagUsage)
	keepGoing := fs.Bool("keep-going", false, "run past a failed step; a step reading a failed step is recorded skipped")
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
	}, latencyPolicy(e), latencySpot), *quiet || *asJSON)
	if err != nil {
		return err
	}
	defer func() { writeGateSidecar(runSidecar(e, c, rec)) }()
	var pinnedSlow []diff.LatencyFlag
	if !*dry && len(c.KeptRed) > 0 {
		judgePinnedDrift(e, c, rec, pinnedRef)
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
		return runVerdict(rec)
	}
	lead := ""
	if !rec.Passed() && rec.KeptRed != runner.KeptRedAsPinned {
		if literal := detectLiteralCollision(e, c, rec); literal != nil {
			lead = literal.line()
		} else if reuse := detectFixtureReuse(e, c, rec); reuse.finding() {
			lead = "FINDING: " + reuse.line()
		} else if reuse != nil {
			lead = reuse.line() + "; " + reuse.rerun("run", rest[0])
		}
	}
	if lead != "" && rec.KeptRed == runner.KeptRedNotAsPinned {
		rec.KeptRedNote = keptRedNotJudged(rec.KeptRedNote)
	}
	if *quiet {
		fmt.Println(runSummary(quietRecord(rec), *dry, false, lead))
		if savedPath != "" && !quietlyGreen(rec) {
			fmt.Printf("  run %s -> %s\n", rec.RunID, savedPath)
		}
	} else {
		fmt.Println(runSummary(rec, *dry, true, lead))
	}
	if line := neverRanLine(c, rec); line != "" && !*dry {
		fmt.Println("  " + line)
	}
	if spot != nil {
		renamed, _ := diff.RenameSpotSteps(spot, rec.Steps)
		for _, f := range latencyFlags(e, renamed, rec, latencyPolicy(e)) {
			fmt.Println("  " + f.Line())
		}
	}
	for _, f := range pinnedSlow {
		fmt.Println("  " + f.Line())
	}
	if step := timedOutStep(rec); step != "" {
		fmt.Printf("  step %q: %s, and run it again\n", step, timeoutRemedy)
	}
	if life := examineTokenLifetime(e, rec); life != nil && !*dry {
		fmt.Println("  " + life.label() + life.line())
		if life.finding() && rec.KeptRed == "" {
			return fmt.Errorf("chain %s: %s", rec.Chain, life.line())
		}
	}
	if loss := examineSessionLoss(e, rec); loss != nil && !*dry {
		fmt.Println("  " + loss.line())
		if loss.finding() && rec.KeptRed == "" {
			return fmt.Errorf("chain %s: %s", rec.Chain, loss.line())
		}
	} else if fresh := repeatedFreshRefusal(e, rec); fresh != nil && !*dry {
		fmt.Println("  " + fresh.line())
		if rec.KeptRed == "" {
			return fmt.Errorf("chain %s: %s", rec.Chain, fresh.line())
		}
	} else if flaky := detectIntermittent(e, rec); flaky != nil && !*dry && !rec.Passed() {
		for _, line := range flaky.notes() {
			fmt.Println("  note: " + line)
		}
		if flaky.finding() {
			fmt.Println("  FINDING: " + flaky.line())
			if others := flaky.otherFailures(rec); len(others) > 0 && rec.KeptRed == "" {
				return fmt.Errorf("chain %s: failed at %s, not an intermittent failure; also %s", rec.Chain, strings.Join(others, ", "), flaky.line())
			}
			return fmt.Errorf("chain %s: %s", rec.Chain, flaky.line())
		}
	}
	if err := runVerdict(rec); err != nil {
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
	"  3  no verdict: unreachable, a gateway answered, a restart mid-run, login or auth refused; re-run\n"

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

func executeChain(ctx context.Context, e *env, c *chain.Chain, opts runner.Options, quiet bool) (*runner.Record, error) {
	r, _, err := runner.NewFromConfig(ctx, e.cfg, e.cat)
	if err != nil {
		return nil, err
	}
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
	return r.Run(ctx, c, opts)
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

func summary(rec *runner.Record, dry bool) string {
	return runSummary(rec, dry, false, "")
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
	return fmt.Sprintf("%d later step(s) were not run (%s): a run stops at its first failure, so whether they "+
		"pass is unknown and this failure may not be the only one; run with -keep-going to see them",
		len(left), capList(left, 10))
}

func runSummary(rec *runner.Record, dry, stepsShown bool, lead string) string {
	var b strings.Builder
	verdict := strings.ToUpper(rec.Status)
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
		}
		fmt.Fprintf(&b, "\n  %s", strings.ReplaceAll(failure, "\n", "\n  "))
	}
	if line := firstFailureRequest(rec); line != "" && !dry {
		fmt.Fprintf(&b, "\n  %s", line)
	}
	if rec.KeptRedNote != "" {
		fmt.Fprintf(&b, "\n  kept red (%s): %s", rec.KeptRed, strings.ReplaceAll(rec.KeptRedNote, "\n", "\n    "))
	}
	for _, line := range warningLines(rec) {
		fmt.Fprintf(&b, "\n  %s", line)
	}
	if len(rec.Exports) > 0 {
		names := make([]string, 0, len(rec.Exports))
		for k := range rec.Exports {
			names = append(names, k)
		}
		sort.Strings(names)
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

func firstFailureRequest(rec *runner.Record) string {
	if rec.KeptRed != "" || rec.Status != runner.StatusFailed {
		return ""
	}
	bad, first := map[string]bool{}, ""
	for _, st := range rec.Steps {
		if st != nil && st.Status != runner.StatusPassed && st.Status != runner.StatusSkipped {
			bad[st.ID] = true
			if first == "" {
				first = st.ID
			}
		}
	}
	if first == "" {
		return ""
	}
	return requestLine(rec, first, bad)
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
			if !containsName(out, c) {
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

func containsName(list []string, name string) bool {
	for _, s := range list {
		if s == name {
			return true
		}
	}
	return false
}
