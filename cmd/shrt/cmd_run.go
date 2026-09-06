package main

import (
	"context"
	"flag"
	"fmt"
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
	keepGoing := fs.Bool("keep-going", false, "run past a step that did not pass; a step reading a failed step's response or exports is recorded skipped, not sent; the run stays failed")
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
		return fmt.Errorf("-var %s names a variable chain %q never reads, so it would have no effect.\n"+
			"A chain isolates its fixtures with vars, so a mistyped one silently collapses every run onto "+
			"the same key. Check the spelling, or drop the flag.\nvars this chain reads: %s",
			strings.Join(unused, ", "), c.Name, strings.Join(c.DeclaredVarNames(), ", "))
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
		return emitJSON(rec)
	}
	fmt.Println(summary(rec, *dry))
	if !rec.Passed() {
		return fmt.Errorf("chain %s: %s", rec.Chain, rec.Status)
	}
	return nil
}

const buildFlagUsage = "stamp this build identity (a version, commit or image tag) into the run record; overrides target.build_header"

func executeChain(ctx context.Context, e *env, c *chain.Chain, opts runner.Options, quiet bool) (*runner.Record, error) {
	r, _, err := runner.NewFromConfig(ctx, e.cfg, e.cat)
	if err != nil {
		return nil, err
	}
	if !quiet {
		r.OnStep = func(sr *runner.StepRecord) {
			fmt.Printf("%-4s %2d %-24s %-52s %4dms\n", statusMark(sr.Status), sr.Index, sr.ID, sr.Call, sr.LatencyMS)
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

func statusMark(s string) string {
	switch s {
	case runner.StatusPassed:
		return "ok"
	case runner.StatusSkipped:
		return "--"
	default:
		return "FAIL"
	}
}

func summary(rec *runner.Record, dry bool) string {
	var b strings.Builder
	verdict := strings.ToUpper(rec.Status)
	if dry && rec.Passed() {
		verdict = "DRY-RUN OK (resolved and validated, nothing sent)"
	}
	fmt.Fprintf(&b, "%s: %s in %dms", rec.Chain, verdict, rec.DurationMS)
	if rec.Build != "" {
		fmt.Fprintf(&b, "\n  build: %s at %s", rec.Build, rec.Target)
	}
	if len(rec.FailedSteps) > 0 {
		fmt.Fprintf(&b, "\n  did not pass: %s", strings.Join(rec.FailedSteps, ", "))
	}
	if rec.Failure != "" {
		fmt.Fprintf(&b, "\n  %s", strings.ReplaceAll(rec.Failure, "\n", "\n  "))
	}
	if len(rec.Exports) > 0 {
		b.WriteString("\n  exports:")
		for k, v := range rec.Exports {
			fmt.Fprintf(&b, " %s=%v", k, v)
		}
	}
	return b.String()
}
