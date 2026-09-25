package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
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
	fs.Var(vars, "var", "override a chain var, repeatable: -var key=value")
	save := fs.Bool("save", true, "persist the run record")
	dry := fs.Bool("dry-run", false, "resolve and validate every request without sending it")
	asJSON := fs.Bool("json", false, "emit the run record as JSON")
	quiet := fs.Bool("quiet", false, "suppress per-step progress; a green chain prints its verdict line only")
	build := fs.String("build", "", buildFlagUsage)
	keepGoing := fs.Bool("keep-going", false, "run past a step that did not pass; a step reading a failed step's response or exports is recorded skipped, not sent, unless it reads a field no failed expectation covers; once the target is unreachable (connection refused, dial or DNS failure) nothing more is sent; the run stays failed")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: shrt run <chain> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(fs.Output(), runExitCodes)
	}
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
	if unused := c.UnusedVarNames(vars); len(unused) > 0 {
		return unusedVarError(unused, c.Name, c.DeclaredVarNames())
	}

	var spot *store.SafeSpot
	if !*dry {
		if loaded, err := e.store.LoadSafeSpot(c.Name); err == nil && loaded.DigestMatches() {
			spot = loaded
		}
	}
	var pinnedRef *runner.Record
	if !*dry && len(c.KeptRed) > 0 {
		pinnedRef = pinnedReference(e, c, &runner.Record{Chain: c.Name, ChainDigest: c.Digest(), Target: e.cfg.Target.BaseURL, StartedAt: time.Now()})
	}
	rec, err := executeChain(ctx, e, c, withLatency(runner.Options{
		Vars: supplied, Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, DryRun: *dry, KeepGoing: *keepGoing, Build: *build,
	}, latencyPolicy(e), spot), *quiet || *asJSON)
	if err != nil {
		return err
	}
	if !*dry && len(c.KeptRed) > 0 {
		judgePinnedDrift(e, c, rec, pinnedRef)
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
	return runVerdict(rec)
}

const runExitCodes = "\nexit codes:\n" +
	"  0  passed\n" +
	"     - a -dry-run: every request resolved and validated\n" +
	"     - a chain with kept_red: failed exactly as kept_red pins, every pin evaluated (the run goes\n" +
	"       past every failure, pinned or not, as -keep-going does) and no step left unsent\n" +
	"  1  failed\n" +
	"     - a step was answered and an expectation did not hold\n" +
	"     - a chain with kept_red failed anywhere else or differently, or passed, so the pinned\n" +
	"       defect is gone, or a pinned step returned something else than in the last run of the\n" +
	"       same chain file that failed as pinned\n" +
	"     - a token the backend accepted earlier in the run and then refused, when the previous run\n" +
	"       that sent that step was refused there the same way and neither run shows a restart (see 3)\n" +
	"     - FINDING: token refused <N>s after issue although the login said it expires in <M>s: the\n" +
	"       re-login's own token refused early too in this run, or a token accepted and then refused\n" +
	"       early in this run and in the previous run, with no restart shown in either (a single\n" +
	"       early refusal is a WARNING line: exit 3 when it left a step unanswered, else 0)\n" +
	"     - a token a login in this run had just issued, refused on its first use, when the previous\n" +
	"       run that sent that step was refused there the same way, with its own freshly issued token\n" +
	"     - intermittent failure at <rpc>: a server error (internal, unknown, resource_exhausted, a 5xx\n" +
	"       with a Connect body...) at a step whose request another step of this run had answered, or\n" +
	"       that the previous run answered while failing at another step with the same error\n" +
	"     - a refusal before anything was sent, checked for every step up front as -dry-run does:\n" +
	"       - bad flags, an unknown chain, a -var the chain never reads, a missing var\n" +
	"       - an unset env var read by a step or by the login body of an auth profile a step runs under\n" +
	"       - a reference to a step or export that does not exist or runs later, or to a response field\n" +
	"         the producing step's message does not declare or a request path its request does not\n" +
	"         declare\n" +
	"       - a reference whose declared type cannot fill the numeric field it is sent in (a bool, enum,\n" +
	"         bytes or timestamp into an int64; a string may hold digits and is only a lint warning)\n" +
	"       - a whole message into a string, bytes, bool, enum or numeric field (name: ${p.product}; a\n" +
	"         Timestamp, Duration, FieldMask or wrapper renders as one value and passes)\n" +
	"       - a whole list or map into a single-valued field or a single value into a list or map\n" +
	"       - an unknown auth profile, an rpc the catalog does not have, a streaming rpc\n" +
	"       - a config or descriptor that does not load, a conventions path no response declares, a\n" +
	"         step body the proto rejects\n" +
	"  3  error: a step could not complete, so the run is not a verdict about the backend\n" +
	"     - an unresolved reference, or a body only invalid with the values a real response gave\n" +
	"     - target unreachable, or the connection closed before a response because the backend\n" +
	"       stopped or crashed\n" +
	"     - a gateway answered for the service with a Connect unavailable or a bare HTTP 502/503/504\n" +
	"     - login failed\n" +
	"     - a token the backend accepted earlier in the run and then refused: a likely restart mid-run.\n" +
	"       A restart shows as a call accepted when re-sent after a fresh login, data created before it\n" +
	"       gone after the re-login, or a step before it with no answer from the service\n" +
	"     - a token a login in this run had just issued and the backend refused on its first use:\n" +
	"       reported as a possible auth regression\n"

func runVerdict(rec *runner.Record) error {
	switch rec.KeptRed {
	case runner.KeptRedAsPinned:
		return nil
	case runner.KeptRedNotAsPinned:
		if rec.KeptRedNew != "" {
			return fmt.Errorf("chain %s: kept red, but it did not fail as pinned: %s", rec.Chain, shortNewFailure(rec.KeptRedNew))
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
	for _, item := range strings.Split(strings.TrimPrefix(rec.KeptRedNew, runner.NewFailurePrefix), "; ") {
		id, _, _ := strings.Cut(item, " ")
		if ids[id] && !seen[id] {
			seen[id] = true
			named = append(named, id)
		}
	}
	if len(named) == 0 {
		return rec.KeptRedNew
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

func unusedVarError(unused []string, chainName string, reads []string) error {
	readsLine := "this chain reads no vars at all, so any -var is rejected"
	if len(reads) > 0 {
		readsLine = "vars this chain reads: " + strings.Join(reads, ", ")
	}
	return fmt.Errorf("-var %s names a variable chain %q never reads, so it would have no effect.\n"+
		"A chain isolates its fixtures with vars, so a mistyped one silently collapses every run onto "+
		"the same key. Check the spelling, or drop the flag.\n%s",
		strings.Join(unused, ", "), chainName, readsLine)
}

const buildFlagUsage = "stamp this build identity (a version, commit or image tag) into the run record; overrides target.build_header"

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
			for _, line := range strings.Split(sr.Warning, "\n") {
				if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, runner.UndeclaredFieldsWarning) {
					continue
				}
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
	if rec.KeptRedNote != "" {
		fmt.Fprintf(&b, "\n  kept red (%s): %s", rec.KeptRed, rec.KeptRedNote)
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
		for _, line := range strings.Split(sr.Warning, "\n") {
			if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, runner.UndeclaredFieldsWarning) {
				continue
			}
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
