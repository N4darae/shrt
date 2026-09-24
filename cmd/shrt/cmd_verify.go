package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func init() {
	register(&command{
		name:    "verify",
		summary: "replay a chain and diff it against its safe spot",
		run:     runVerify,
	})
}

func runVerify(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	vars := varFlags{}
	fs.Var(vars, "var", "override a chain var, repeatable: -var key=value")
	useRun := fs.String("run", "", "diff a recorded run id instead of replaying")
	asJSON := fs.Bool("json", false, "emit the diff report as JSON")
	quiet := fs.Bool("quiet", false, "suppress per-step progress")
	save := fs.Bool("save", true, "persist the replay record")
	build := fs.String("build", "", buildFlagUsage)
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
	spot, err := e.store.LoadSafeSpot(name)
	if err != nil {
		if e.store.HasProposal(name) {
			return fmt.Errorf("%w\na proposal for %s awaits a person's decision: shrt confirm %s -approve -by <name>, or -reject", err, name, name)
		}
		return fmt.Errorf("%w\nno safe spot yet — run the chain, check the responses, propose it with 'shrt confirm %s -note \"...\"', and a person approves it", err, name)
	}

	var rec *runner.Record
	if *useRun != "" {
		if *useRun == spot.RunID {
			fmt.Fprintf(os.Stderr,
				"verify: run %s IS the run this safe spot was made from, so this compares a recording with "+
					"itself. It is a clean control for the differ, and it is NOT evidence about the backend: "+
					"it reports no drift whatever the backend now does. Pass a LATER run id, or drop -run to "+
					"replay live.\n", *useRun)
		}
		rec, err = e.store.LoadRun(name, *useRun)
	} else {
		var c *chain.Chain
		c, err = chain.Resolve(e.chainsDir(), name)
		if err != nil {
			return err
		}
		rec, err = executeChain(ctx, e, c, runner.Options{Vars: c.CoerceVars(vars), Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, Build: *build}, *quiet || *asJSON)
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
	if *asJSON {
		if err := emitJSON(map[string]any{"run": rec, "diff": report}); err != nil {
			return err
		}
	} else {
		fmt.Println()
		if spot.Build != "" || rec.Build != "" {
			fmt.Printf("safe spot build %s, this run build %s\n", orUnknown(spot.Build), orUnknown(rec.Build))
		}
		fmt.Println(report.Text())
		if report.Clean() {
			fmt.Printf("covers the %d step(s) of this chain only; a regression in a path no safe spot exercises is not seen\n", len(spot.Steps))
		}
	}
	if !report.Clean() {
		return fmt.Errorf("regression: %d change(s) vs safe spot", len(report.Changes))
	}
	if !rec.Passed() {
		return fmt.Errorf("chain %s: %s", rec.Chain, rec.Status)
	}
	return nil
}

func orUnknown(s string) string {
	if s == "" {
		return "(not recorded)"
	}
	return s
}
