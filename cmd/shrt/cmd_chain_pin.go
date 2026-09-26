package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

const pinUsage = "usage: shrt chain pin <chain>   keep a red chain's failing steps red in their own slice and leave them out of the chain;\n" +
	"       it re-runs the chain with -keep-going first when its newest run did not reach every step"

func chainPin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("chain pin", flag.ContinueOnError)
	setUsage(fs, pinUsage, "\nexit codes:\n  0  pinned: the slice reproduced the failure and is written kept red, the chain rewritten without it\n"+
		"  1  refused: nothing failed, kept_red already declared, a FINDING or intermittent failure explains the red,\n"+
		"     or the slice did not reproduce the failure\n")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New(pinUsage)
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	c, err := e.resolveChain(rest[0])
	if err != nil {
		return err
	}
	ref := sliceChainRef(rest[0], c)
	if len(c.KeptRed) > 0 {
		return fmt.Errorf("%s already declares kept_red, so it is pinned: shrt run %s says whether it still fails as pinned", c.Name, ref)
	}
	if c.SourcePath == "" {
		return fmt.Errorf("%s has no source file to rewrite", c.Name)
	}
	rec, _ := newestRecordReaching(e, c.Name, "", "", false)
	if rec == nil || rec.ChainDigest != c.Digest() || !ranEveryStep(c, rec) {
		out, runErr := quietly(func() error { return runRun(ctx, []string{ref, "-keep-going", "-quiet"}) })
		next, _ := newestRecordReaching(e, c.Name, "", "", false)
		if next == nil || rec != nil && next.RunID == rec.RunID {
			fmt.Print(out)
			if runErr == nil {
				runErr = fmt.Errorf("the -keep-going run of %s left no record", c.Name)
			}
			return runErr
		}
		rec = next
		fmt.Printf("ran %s -keep-going: run %s, %s\n", ref, rec.RunID, rec.Status)
	}
	if rec.Passed() {
		fmt.Printf("nothing to pin: run %s of %s passed\n", rec.RunID, c.Name)
		return nil
	}
	if why := pinBlocker(e, c, rec); why != "" {
		return fmt.Errorf("not pinned: run %s of %s is explained by what shrt run reports, not a defect to keep red: %s", rec.RunID, c.Name, why)
	}
	steps := expectationFailures(rec)
	if len(steps) == 0 {
		return fmt.Errorf("not pinned: no step of run %s failed an expectation, and kept_red pins failed expectations only", rec.RunID)
	}
	if other := slicesWithout(failedSteps(rec), steps); len(other) > 0 {
		return fmt.Errorf("not pinned: %s in run %s errored rather than failed an expectation, so no kept_red can pin it: shrt run %s says why",
			strings.Join(other, ", "), rec.RunID, ref)
	}
	slicePath := filepath.Join(sliceDir(e, c), chain.DefaultSliceName(c.Name, steps[0])+".yaml")
	keep, sliceOut := "", ""
	var pinned *chain.Chain
	for attempt := 1; ; attempt++ {
		before, _ := os.ReadFile(slicePath)
		args := []string{ref, "-step", steps[0], "-kept-red=" + strings.Join(steps, ","), "-verify", "-write", "-run", rec.RunID}
		if keep != "" {
			args = append(args, "-keep", keep)
		}
		sliceOut, err = quietly(func() error { return chainSlice(ctx, args) })
		after, _ := os.ReadFile(slicePath)
		loaded, loadErr := chain.LoadFile(slicePath)
		if err == nil && loadErr == nil && len(loaded.KeptRed) > 0 && string(after) != string(before) {
			pinned = loaded
			break
		}
		if err == nil {
			err = fmt.Errorf("the slice of %s was not written kept red", steps[0])
		}
		next := suggestedKeep(sliceOut)
		if next == "" || next == keep || attempt == 3 {
			break
		}
		fmt.Printf("slice of %s did not reproduce without the writes it needs: again with -keep %s\n", steps[0], next)
		keep = next
	}
	if pinned == nil {
		fmt.Print(sliceOut)
		return fmt.Errorf("not pinned, %s left as it was: %v", c.Name, err)
	}
	withoutOut, err := quietly(func() error {
		return sliceWithout(ref, steps, "", &optionalString{set: true}, sourceFileArg(c), false, false)
	})
	if err != nil {
		fmt.Print(withoutOut)
		return fmt.Errorf("%s is written kept red, but %s was not rewritten without %s: %v", shownPath(slicePath), c.Name, strings.Join(steps, ", "), err)
	}
	fmt.Printf("wrote %s: kept red on %s\n", shownPath(slicePath), pinList(pinned, pinned.KeptRed))
	fmt.Printf("wrote %s: the pinned step(s) left out\n", shownPath(c.SourcePath))
	for _, line := range strings.Split(sliceOut, "\n") {
		if strings.HasPrefix(line, "verify ") {
			fmt.Println(line)
		}
	}
	return nil
}

func suggestedKeep(out string) string {
	for _, line := range strings.Split(out, "\n") {
		_, cmd, ok := strings.Cut(line, "next: shrt chain slice ")
		if !ok || strings.Contains(cmd, "<fresh>") {
			continue
		}
		fields := strings.Fields(cmd)
		for i, f := range fields {
			if f == "-keep" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
	}
	return ""
}

func ranEveryStep(c *chain.Chain, rec *runner.Record) bool {
	for _, s := range c.Steps {
		if _, ok := rec.Step(s.ID); !ok {
			return false
		}
	}
	return true
}

func expectationFailures(rec *runner.Record) []string {
	out := []string{}
	for _, st := range rec.Steps {
		if st == nil || st.Status != runner.StatusFailed {
			continue
		}
		for _, x := range st.Expect {
			if !x.Passed && x.Rule != "unevaluated" {
				out = append(out, st.ID)
				break
			}
		}
	}
	return out
}

func slicesWithout(all, drop []string) []string {
	out := []string{}
	for _, s := range all {
		if !containsStr(drop, s) {
			out = append(out, s)
		}
	}
	return out
}

func pinBlocker(e *env, c *chain.Chain, rec *runner.Record) string {
	if literal := detectLiteralCollision(e, c, rec); literal != nil {
		return literal.line()
	}
	if reuse := detectFixtureReuse(e, c, rec); reuse != nil {
		return reuse.line()
	}
	if life := examineTokenLifetime(e, rec); life != nil {
		return life.line()
	}
	if loss := examineSessionLoss(e, rec); loss != nil {
		return loss.line()
	}
	if fresh := repeatedFreshRefusal(e, rec); fresh != nil {
		return fresh.line()
	}
	if flaky := detectIntermittent(e, rec); flaky != nil {
		return flaky.line()
	}
	return ""
}

func quietly(fn func() error) (string, error) {
	saved := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", fn()
	}
	os.Stdout = w
	done := make(chan []byte, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- out
	}()
	runErr := fn()
	_ = w.Close()
	os.Stdout = saved
	out := <-done
	_ = r.Close()
	return string(out), runErr
}
