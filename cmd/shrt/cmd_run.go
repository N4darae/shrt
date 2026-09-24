package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
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
	quiet := fs.Bool("quiet", false, "suppress per-step progress")
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
	c, err := chain.Resolve(e.chainsDir(), rest[0])
	if err != nil {
		return err
	}

	supplied := c.CoerceVars(vars)
	if unused := c.UnusedVarNames(vars); len(unused) > 0 {
		return unusedVarError(unused, c.Name, c.DeclaredVarNames())
	}

	rec, err := executeChain(ctx, e, c, runner.Options{
		Vars: supplied, Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, DryRun: *dry, KeepGoing: *keepGoing, Build: *build,
	}, *quiet || *asJSON)
	if err != nil {
		return err
	}
	if *save && !*dry {
		path, err := e.store.SaveRun(rec)
		if err != nil {
			return err
		}
		if !*asJSON {
			fmt.Printf("\nrun %s -> %s\n", rec.RunID, path)
		}
	}
	if *asJSON {
		if err := emitJSON(rec); err != nil {
			return err
		}
		return runVerdict(rec)
	}
	fmt.Println(summary(rec, *dry))
	return runVerdict(rec)
}

const runExitCodes = "\nexit codes:\n" +
	"  0  passed (a -dry-run: every request resolved and validated); for a chain with kept_red, failed\n" +
	"     exactly as kept_red pins\n" +
	"  1  failed: a step was answered and an expectation did not hold (for a chain with kept_red: it\n" +
	"     failed anywhere else or differently, or passed, so the pinned defect is gone); also a refusal before anything\n" +
	"     was sent (bad flags, an unknown chain, a -var the chain never reads, a missing var,\n" +
	"     an unset env var read by a step or by the login body of an auth profile a step runs under,\n" +
	"     a reference to a step or export that does not exist or runs later, or to a response field\n" +
	"     the producing step's message does not declare or a request path its request does not\n" +
	"     declare, a reference whose declared type cannot fill the numeric field it is sent in (a\n" +
	"     bool, enum, bytes or timestamp into an int64; a string may hold digits and is only a lint\n" +
	"     warning), a whole list or map into a single-valued field or a single value into a list or\n" +
	"     map, an unknown auth profile, an rpc the catalog does not have, a streaming rpc, a\n" +
	"     config or descriptor that does not load, a conventions path no response declares, a step\n" +
	"     body the proto rejects, checked for every step up front as -dry-run does)\n" +
	"  3  error: a step could not complete (unresolved reference, a body only invalid with the values\n" +
	"     a real response gave, target unreachable, the connection closed before a response because\n" +
	"     the backend stopped or crashed, login failed), so the run is not a verdict about the backend\n"

func runVerdict(rec *runner.Record) error {
	switch rec.KeptRed {
	case runner.KeptRedAsPinned:
		return nil
	case runner.KeptRedNotAsPinned:
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
		unreachableShown := false
		r.OnStep = func(sr *runner.StepRecord) {
			if sr.NotSentUnreachable() {
				if !unreachableShown {
					unreachableShown = true
					fmt.Printf("%-5s %2d.. every remaining step %s\n", statusMark(sr.Status, opts.DryRun), sr.Index, sr.Error)
				}
				return
			}
			fmt.Println(progressLine(sr, opts.DryRun))
			for _, ex := range sr.Expect {
				if !ex.Passed {
					fmt.Printf("       %s\n", ex.String())
				}
			}
			if sr.Error != "" {
				fmt.Printf("       %s\n", sr.Error)
			}
			if sr.Warning != "" {
				fmt.Printf("       %s\n", sr.Warning)
			}
		}
	}
	return r.Run(ctx, c, opts)
}

func progressLine(sr *runner.StepRecord, dry bool) string {
	return fmt.Sprintf("%-5s %2d %-24s %-52s %4dms", statusMark(sr.Status, dry), sr.Index, sr.ID, sr.Call, sr.LatencyMS)
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
	var b strings.Builder
	verdict := strings.ToUpper(rec.Status)
	if dry && rec.Passed() {
		verdict = "DRY-RUN OK (resolved and validated, nothing sent)"
	}
	if rec.KeptRed == runner.KeptRedAsPinned {
		verdict = "FAILED AS PINNED (kept red)"
	}
	fmt.Fprintf(&b, "%s: %s in %dms", rec.Chain, verdict, rec.DurationMS)
	if rec.Build != "" {
		fmt.Fprintf(&b, "\n  build: %s at %s", rec.Build, rec.Target)
	}
	if len(rec.FailedSteps) > 0 {
		fmt.Fprintf(&b, "\n  did not pass: %s", capList(rec.FailedSteps, 10))
	}
	if rec.Failure != "" {
		fmt.Fprintf(&b, "\n  %s", strings.ReplaceAll(rec.Failure, "\n", "\n  "))
	}
	if rec.KeptRedNote != "" {
		fmt.Fprintf(&b, "\n  kept red (%s): %s", rec.KeptRed, rec.KeptRedNote)
	}
	if rec.Warning != "" {
		fmt.Fprintf(&b, "\n  warning: %s", rec.Warning)
	}
	for _, sr := range rec.Steps {
		if sr != nil && strings.TrimSpace(sr.Warning) != "" {
			fmt.Fprintf(&b, "\n  warning [%s]: %s", sr.ID, sr.Warning)
		}
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
