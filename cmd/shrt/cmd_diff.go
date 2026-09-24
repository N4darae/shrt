package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func init() {
	register(&command{
		name:    "diff",
		summary: "compare two recorded runs of a chain step by step, with no safe spot",
		run:     runDiff,
	})
}

const diffUsage = "usage: shrt diff <run-a> <run-b>\n" +
	"       shrt diff <chain> <run-a> <run-b>   run ids, 'latest', or 'latest~N' (N runs before latest)\n" +
	"       shrt diff <chain>                   latest~1 against latest"

func runDiff(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the comparison as JSON")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	e, err := loadEnv(false)
	if err != nil {
		return err
	}
	var a, b *runner.Record
	switch len(rest) {
	case 1:
		a, err = selectRun(e, rest[0], "latest~1")
		if err == nil {
			b, err = selectRun(e, rest[0], "latest")
		}
	case 2:
		a, err = findRunAnywhere(e, rest[0])
		if err == nil {
			b, err = findRunAnywhere(e, rest[1])
		}
		if err == nil && a.Chain != b.Chain {
			err = fmt.Errorf("run %s is of chain %s and run %s is of chain %s: shrt diff compares two runs of the SAME chain",
				a.RunID, a.Chain, b.RunID, b.Chain)
		}
	case 3:
		a, err = selectRun(e, rest[0], rest[1])
		if err == nil {
			b, err = selectRun(e, rest[0], rest[2])
		}
	default:
		return fmt.Errorf("%s", diffUsage)
	}
	if err != nil {
		return err
	}
	if a.RunID == b.RunID {
		return fmt.Errorf("both sides are run %s: comparing a record with itself says nothing", a.RunID)
	}
	rep := diff.CompareRuns(a, b)
	if *asJSON {
		if err := emitJSON(rep); err != nil {
			return err
		}
	} else {
		fmt.Println(rep.Text())
	}
	if !rep.Same() {
		return exitWith(1, "runs %s and %s differ (a comparison between two runs, not a regression verdict)", a.RunID, b.RunID)
	}
	return nil
}

func selectRun(e *env, chainName, sel string) (*runner.Record, error) {
	back, isSelector, err := parseLatest(sel)
	if err != nil {
		return nil, err
	}
	if !isSelector {
		return e.store.LoadRun(chainName, sel)
	}
	ids, err := e.store.ListRuns(chainName)
	if err != nil {
		return nil, err
	}
	if back >= len(ids) {
		return nil, fmt.Errorf("%s needs %d run(s) of chain %s, and %d are recorded", sel, back+1, chainName, len(ids))
	}
	return e.store.LoadRun(chainName, ids[len(ids)-1-back])
}

func parseLatest(sel string) (int, bool, error) {
	if sel == "latest" {
		return 0, true, nil
	}
	rest, ok := strings.CutPrefix(sel, "latest~")
	if !ok {
		return 0, false, nil
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return 0, false, fmt.Errorf("%q: want latest or latest~N with N a whole number", sel)
	}
	return n, true, nil
}

func findRunAnywhere(e *env, id string) (*runner.Record, error) {
	if _, isSelector, _ := parseLatest(id); isSelector {
		return nil, fmt.Errorf("%s names a run only within a chain\n\n%s", id, diffUsage)
	}
	found, err := e.store.FindRun(id)
	if err != nil {
		return nil, err
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("no run %s under %s\n\n%s", id, e.store.RunsDir, diffUsage)
	case 1:
		return found[0], nil
	}
	return nil, fmt.Errorf("run id %s is recorded under %d chains; name the chain: shrt diff <chain> <run-a> <run-b>", id, len(found))
}
