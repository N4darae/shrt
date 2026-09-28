package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
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
	"       shrt diff <chain>                   the latest run, skipping a verify replay recorded right after a run, vs the latest earlier one that is not a replay"

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
	step := fs.String("step", "", "print step `id` of one run of <chain> (default latest) as recorded: its request and response, compact JSON")
	listMasked := fs.Bool("masked", false, "list every difference kept out of the comparison, with both values and what hid it: the volatile pattern, an id- or timestamp-shaped value, a renamed id or a fixture echo (masked_changes under -json)")
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
	if len(rest) == 1 || len(rest) == 3 || *step != "" && len(rest) == 2 {
		if err := e.knownChain(rest[0]); err != nil {
			return err
		}
	}
	if *step != "" {
		return showStep(e, rest, *step)
	}
	var a, b *runner.Record
	switch len(rest) {
	case 1:
		a, b, err = latestNonReplays(e, rest[0])
	case 2:
		a, err = findRunAnywhere(e, rest[0])
		if err == nil {
			b, err = findRunAnywhere(e, rest[1])
		}
		if err != nil {
			if named := chainArgs(e, rest); named != nil {
				return named
			}
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
	c, err := chain.Resolve(e.chainsDir(), a.Chain)
	if err != nil && a.ChainSource != "" {
		c, err = chain.LoadFile(a.ChainSource)
	}
	if err == nil {
		fx = requestFixtures(c)
	}
	rep := diff.CompareRunsSkipping(a, b, currentVolatile(e, a.Chain), fx)
	rep.DropUnsentDefaults(a, b, unsentDefault(e))
	if len(rest) == 3 {
		rep.SelectorA, rep.SelectorB = rest[1], rest[2]
	}
	masked := rep.MaskedList()
	if !*listMasked {
		rep.MaskedChanges = nil
	}
	if *asJSON {
		if err := emitJSON(rep); err != nil {
			return err
		}
	} else {
		fmt.Println(rep.Text())
		if *listMasked {
			fmt.Println("\n" + masked)
		}
	}
	if !rep.Same() {
		return shownError{exitWith(1, "runs %s and %s differ", a.RunID, b.RunID)}
	}
	return nil
}

func latestNonReplays(e *env, chainName string) (*runner.Record, *runner.Record, error) {
	ids, err := e.store.ListRuns(chainName)
	if err != nil {
		return nil, nil, err
	}
	recs := make([]*runner.Record, len(ids))
	for i, id := range ids {
		if recs[i], err = e.store.LoadRun(chainName, id); err != nil {
			return nil, nil, err
		}
	}
	picked, replays := []*runner.Record{}, 0
	for i := len(recs) - 1; i >= 0 && len(picked) < 2; i-- {
		if recs[i].ReplayOf != "" && (len(picked) > 0 || i > 0 && replayBesideRun(recs[i-1], recs[i])) {
			replays++
			continue
		}
		picked = append(picked, recs[i])
	}
	if len(picked) < 2 {
		return nil, nil, fmt.Errorf("chain %s has %d recorded run(s) that are not shrt verify replays (%d replay(s) skipped), and a "+
			"default diff needs two; name the runs to compare: shrt diff %s <run-a> <run-b> (latest and latest~N count replays too)",
			chainName, len(picked), replays, chainName)
	}
	return picked[1], picked[0], nil
}

func replayBesideRun(prev, rec *runner.Record) bool {
	return rec.ReplayOf != "" && prev.ReplayOf == ""
}

func currentVolatile(e *env, chainName string) []string {
	out := append([]string{}, e.cfg.Volatile...)
	c, err := chain.Resolve(e.chainsDir(), chainName)
	if err != nil {
		return out
	}
	return append(out, c.Volatile...)
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

func chainArgs(e *env, args []string) error {
	chains := []string{}
	for _, a := range args {
		if isChainName(e, a) {
			chains = append(chains, a)
		}
	}
	switch len(chains) {
	case 0:
		return nil
	case len(args):
		return fmt.Errorf("%s and %s are chain names, not run ids: shrt diff compares two runs of ONE chain, not two chains. "+
			"Run 'shrt diff %s' for its two latest runs, or 'shrt diff %s <run-a> <run-b>'\n\n%s",
			args[0], args[1], args[0], args[0], diffUsage)
	}
	return fmt.Errorf("%s is a chain name: with two arguments both are run ids. Name the chain first and then its two runs: "+
		"'shrt diff %s <run-a> <run-b>' (ids, latest or latest~N), or 'shrt diff %s' for its two latest runs\n\n%s",
		chains[0], chains[0], chains[0], diffUsage)
}

func isChainName(e *env, name string) bool {
	for _, n := range chain.Names(e.chainsDir()) {
		if n == name {
			return true
		}
	}
	return false
}

func showStep(e *env, rest []string, id string) error {
	if len(rest) < 1 || len(rest) > 2 {
		return fmt.Errorf("usage: shrt diff <chain> [<run>] -step <id>")
	}
	sel := "latest"
	if len(rest) == 2 {
		sel = rest[1]
	}
	rec, err := selectRun(e, rest[0], sel)
	if err != nil {
		return err
	}
	st, ok := rec.Step(id)
	if !ok || st == nil {
		ids := []string{}
		for _, s := range rec.Steps {
			if s != nil {
				ids = append(ids, s.ID)
			}
		}
		return fmt.Errorf("run %s of %s has no step %q; its steps: %s", rec.RunID, rec.Chain, id, capList(ids, 12))
	}
	as := ""
	if st.AuthProfile != "" && st.AuthProfile != "default" {
		as = " as " + st.AuthProfile
	}
	fmt.Printf("%s (%s)%s %s in run %s\n", st.ID, shortRPC(st.Call), as, st.Status, rec.RunID)
	for _, part := range []struct {
		name string
		raw  []byte
	}{{"request", st.Request}, {"response", st.Response}} {
		var buf bytes.Buffer
		if len(part.raw) == 0 || json.Compact(&buf, part.raw) != nil {
			buf.Reset()
			buf.WriteString("(none)")
		}
		fmt.Printf("%s %s\n", part.name, buf.String())
	}
	if st.Error != "" {
		fmt.Println("error " + st.Error)
	}
	return nil
}
