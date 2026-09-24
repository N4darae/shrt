package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
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
	"       shrt diff <chain>                   the two latest runs that are not shrt verify replays"

func runDiff(ctx context.Context, args []string) error {
	err := compareRuns(ctx, args)
	var coded *exitError
	if err == nil || errors.Is(err, flag.ErrHelp) || errors.As(err, &coded) {
		return err
	}
	return &exitError{code: 2, err: err}
}

func compareRuns(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the comparison as JSON")
	setUsage(fs, diffUsage, "\nexit codes:\n  0  the two runs do not differ\n  1  they differ; also, as for every command, "+
		"a flag that cannot be parsed or a setup that cannot load\n"+
		"  2  could not compare: an unknown run, runs of two chains, or the wrong number of arguments\n")
	rest, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return &exitError{code: 1, err: err}
	}
	e, err := loadEnv(false)
	if err != nil {
		return &exitError{code: 1, err: err}
	}
	if len(rest) == 1 || len(rest) == 3 {
		if err := e.knownChain(rest[0]); err != nil {
			return err
		}
	}
	var a, b *runner.Record
	picked := ""
	switch len(rest) {
	case 1:
		a, b, picked, err = latestNonReplays(e, rest[0])
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
	var fx diff.Fixtures
	if c, err := chain.Resolve(e.chainsDir(), a.Chain); err == nil {
		fx = requestFixtures(c)
	}
	rep := diff.CompareRunsSkipping(a, b, currentVolatile(e, a.Chain), fx)
	if *asJSON {
		if picked != "" {
			fmt.Fprintln(os.Stderr, "diff: "+picked)
		}
		if err := emitJSON(rep); err != nil {
			return err
		}
	} else {
		if picked != "" {
			fmt.Println(picked)
		}
		fmt.Println(rep.Text())
	}
	if !rep.Same() {
		return exitWith(1, "runs %s and %s differ (a comparison between two runs, not a regression verdict)", a.RunID, b.RunID)
	}
	return nil
}

func latestNonReplays(e *env, chainName string) (*runner.Record, *runner.Record, string, error) {
	ids, err := e.store.ListRuns(chainName)
	if err != nil {
		return nil, nil, "", err
	}
	picked := []*runner.Record{}
	replays := []string{}
	for i := len(ids) - 1; i >= 0 && len(picked) < 2; i-- {
		rec, err := e.store.LoadRun(chainName, ids[i])
		if err != nil {
			return nil, nil, "", err
		}
		if rec.ReplayOf != "" {
			replays = append(replays, rec.RunID)
			continue
		}
		picked = append(picked, rec)
	}
	if len(picked) < 2 {
		return nil, nil, "", fmt.Errorf("chain %s has %d recorded run(s) that are not shrt verify replays (%d replay(s) skipped), and a "+
			"default diff needs two; name the runs to compare: shrt diff %s <run-a> <run-b> (latest and latest~N count replays too)",
			chainName, len(picked), len(replays), chainName)
	}
	line := fmt.Sprintf("comparing the two latest runs of %s that are not shrt verify replays: run A %s, run B %s",
		chainName, picked[1].RunID, picked[0].RunID)
	if len(replays) > 0 {
		line += fmt.Sprintf(" (skipped %d verify replay(s) of the safe spot, recorded by shrt verify beside a run against the same "+
			"backend: %s; name one to compare it: shrt diff %s <run-a> <run-b>)", len(replays), capList(replays, 3), chainName)
	}
	return picked[1], picked[0], line, nil
}

func currentVolatile(e *env, chainName string) []string {
	out := append([]string{}, e.cfg.Volatile...)
	c, err := chain.Resolve(e.chainsDir(), chainName)
	if err != nil {
		return out
	}
	out = append(out, c.Volatile...)
	for _, s := range c.Steps {
		out = append(out, s.Volatile...)
	}
	return out
}

func selectRun(e *env, chainName, sel string) (*runner.Record, error) {
	back, isSelector, err := parseLatest(sel)
	if err != nil {
		return nil, err
	}
	if !isSelector {
		rec, err := e.store.LoadRun(chainName, sel)
		if err == nil || !errors.Is(err, fs.ErrNotExist) {
			return rec, err
		}
		if found, ferr := e.store.FindRun(sel); ferr == nil && len(found) > 0 {
			return nil, fmt.Errorf("run %s is not a run of chain %s: it is recorded under chain %s, and shrt diff compares two runs of the SAME chain",
				sel, chainName, found[0].Chain)
		}
		ids, _ := e.store.ListRuns(chainName)
		newest := []string{}
		for i := len(ids) - 1; i >= 0 && len(newest) < 3; i-- {
			newest = append(newest, ids[i])
		}
		if len(newest) == 0 {
			return nil, fmt.Errorf("chain %s has no run %s, and no runs are recorded for it", chainName, sel)
		}
		return nil, fmt.Errorf("chain %s has no run %s (recorded, newest first: %s)", chainName, sel, strings.Join(newest, ", "))
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
