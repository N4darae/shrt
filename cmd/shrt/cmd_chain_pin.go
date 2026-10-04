package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

const pinUsage = "usage: shrt chain pin <chain>   keep each defect of a red chain red in a slice of its own and leave it out of the chain,\n" +
	"       re-running the chain with -keep-going first when its newest run did not reach every step and after each rewrite until it passes"

func chainPin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("chain pin", flag.ContinueOnError)
	setUsage(fs, pinUsage, "\nexit codes:\n  0  pinned: each slice reproduced its failure and is written kept red, the rewritten chain passed\n"+
		"  1  refused or stopped: nothing failed, kept_red already declared, a FINDING or intermittent failure explains the red,\n"+
		"     or a slice did not reproduce the failure\n")
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
	written, moved := []string{}, []string{}
	source := shownPath(c.SourcePath)
	report := func() {
		if len(moved) > 0 {
			fmt.Printf("wrote %s: %s no longer runs %s\n", source, c.Name, strings.Join(moved, "; "))
		}
	}
	for round := 0; round <= len(c.Steps); round++ {
		if round > 0 {
			if c, err = e.resolveChain(rest[0]); err != nil {
				return err
			}
		}
		done, gone, err := pinRound(ctx, e, c, ref, round)
		if done != "" {
			written, moved = append(written, done), append(moved, gone)
			continue
		}
		report()
		if err != nil && len(written) > 0 {
			return fmt.Errorf("%s is still red after pinning %s: %v", c.Name, strings.Join(written, ", "), err)
		}
		return err
	}
	report()
	return fmt.Errorf("%s is still red after pinning %s", c.Name, strings.Join(written, ", "))
}

func movedSteps(c, slice *chain.Chain, steps []string) (string, string) {
	removed := steps
	if w, err := chain.Without(c, steps, ""); err == nil {
		removed = nil
		for _, r := range w.Removed {
			removed = append(removed, r.ID)
		}
	}
	held, lost := []string{}, []string{}
	for _, id := range removed {
		if _, ok := slice.Step(id); ok {
			held = append(held, id)
		} else {
			lost = append(lost, id)
		}
	}
	out := strings.Join(held, ", ") + ": kept red in " + slice.Name
	line := "Kept red in " + slice.Name + ": " + strings.Join(held, ", ") + "."
	if len(lost) > 0 {
		out += "; " + strings.Join(lost, ", ") + ": in no slice"
		line += " In no slice: " + strings.Join(lost, ", ") + "."
	}
	return out, line
}

func noteMovedSteps(path, line string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if edited, ok := chain.AppendDescriptionLine(raw, path, line); ok {
		return os.WriteFile(path, edited, 0o644)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		return err
	}
	c.Description = strings.TrimSpace(strings.TrimSpace(c.Description) + "\n" + line)
	return writeSliceFile(path, c)
}

func pinRound(ctx context.Context, e *env, c *chain.Chain, ref string, round int) (string, string, error) {
	rec, _ := newestRecordReaching(e, c.Name, "", "", false)
	if rec == nil || rec.ChainDigest != c.Digest() || !ranEveryStep(c, rec) {
		out, runErr := quietly(func() error { return runRun(ctx, []string{ref, "-keep-going", "-quiet"}) })
		next, _ := newestRecordReaching(e, c.Name, "", "", false)
		if next == nil || rec != nil && next.RunID == rec.RunID {
			fmt.Print(out)
			if runErr == nil {
				runErr = fmt.Errorf("the -keep-going run of %s left no record", c.Name)
			}
			return "", "", runErr
		}
		rec = next
		fmt.Printf("ran %s -keep-going: run %s, %s\n", ref, rec.RunID, rec.Status)
	}
	if rec.Passed() {
		if round == 0 {
			fmt.Printf("nothing to pin: run %s of %s passed\n", rec.RunID, c.Name)
		}
		return "", "", nil
	}
	if why := pinBlocker(e, c, rec); why != "" {
		return "", "", fmt.Errorf("not pinned: run %s of %s is explained by what shrt run reports, not a defect to keep red: %s", rec.RunID, c.Name, why)
	}
	failing := expectationFailures(rec)
	if len(failing) == 0 {
		return "", "", fmt.Errorf("not pinned: no step of run %s failed an expectation, and kept_red pins failed expectations only", rec.RunID)
	}
	if other := slicesWithout(failedSteps(rec), failing); len(other) > 0 {
		return "", "", fmt.Errorf("not pinned: %s in run %s errored rather than failed an expectation, so no kept_red can pin it: shrt run %s says why",
			strings.Join(other, ", "), rec.RunID, ref)
	}
	steps := pinGroup(c, rec, failing)
	slicePath := filepath.Join(sliceDir(e, c), chain.DefaultSliceName(c.Name, steps[0])+".yaml")
	keep, sliceOut := "", ""
	var pinned *chain.Chain
	var err error
	for attempt := 1; ; attempt++ {
		before, _ := os.ReadFile(slicePath)
		args := []string{ref, "-step", steps[0], "-verify", "-write", "-run", rec.RunID}
		if keep != "" {
			args = append(args, "-keep", keep)
		}
		red := &sliceKeptRed{on: true, steps: steps}
		sliceOut, err = quietly(func() error { return sliceChain(ctx, args, red) })
		after, _ := os.ReadFile(slicePath)
		loaded, loadErr := chain.LoadFile(slicePath)
		if err == nil && loadErr == nil && len(loaded.KeptRed) > 0 && string(after) != string(before) {
			pinned = loaded
			break
		}
		if err == nil {
			err = fmt.Errorf("the slice of %s was not written kept red", steps[0])
		}
		next := ""
		if v := red.verdict; v != nil && !strings.Contains(v.Next, "<fresh>") {
			next = strings.Join(v.nextKeep, ",")
		}
		if next == "" || next == keep || attempt == 3 {
			break
		}
		fmt.Printf("slice of %s did not reproduce without the writes it needs: again with -keep %s\n", steps[0], next)
		keep = next
	}
	if pinned == nil {
		fmt.Print(sliceOut)
		return "", "", fmt.Errorf("not pinned, %s left as it was: %v", c.Name, err)
	}
	withoutOut, err := quietly(func() error {
		return sliceWithout(ctx, ref, steps, "", &optionalString{set: true}, sourceFileArg(c), false, nil)
	})
	if err != nil {
		fmt.Print(withoutOut)
		return "", "", fmt.Errorf("%s is written kept red, but %s was not rewritten without %s: %v", shownPath(slicePath), c.Name, strings.Join(steps, ", "), err)
	}
	gone, line := movedSteps(c, pinned, steps)
	if err := noteMovedSteps(c.SourcePath, line); err != nil {
		return "", "", fmt.Errorf("%s is written kept red and %s rewritten without %s, but its description was not updated: %v",
			shownPath(slicePath), c.Name, strings.Join(steps, ", "), err)
	}
	fmt.Printf("wrote %s: kept red on %s\n", shownPath(slicePath), pinList(pinned, pinned.KeptRed))
	for _, line := range strings.Split(sliceOut, "\n") {
		if strings.HasPrefix(line, "verify ") {
			fmt.Println(line)
		}
	}
	return shownPath(slicePath), gone, nil
}

func pinGroup(c *chain.Chain, rec *runner.Record, failing []string) []string {
	steps := []string{failing[0]}
	readers := map[string]bool{}
	if w, err := chain.Without(c, steps, ""); err == nil {
		for _, r := range w.Removed {
			readers[r.ID] = true
		}
	}
	same := failureShape(rec, failing[0])
	for _, id := range failing[1:] {
		if readers[id] || failureShape(rec, id) == same {
			steps = append(steps, id)
		}
	}
	lastWrite := ""
	for _, s := range c.Steps {
		switch {
		case !chain.IsReadOnlyCall(s.Call):
			lastWrite = s.ID
		case slices.Contains(steps, lastWrite) && slices.Contains(failing, s.ID) && !slices.Contains(steps, s.ID):
			steps = append(steps, s.ID)
		}
	}
	return steps
}

func failureShape(rec *runner.Record, id string) string {
	st, _ := rec.Step(id)
	if field := unappliedFilter(st); field != "" {
		return st.Call + "\x00filter " + field
	}
	paths := []string{st.Call}
	for _, x := range st.Expect {
		if !x.Passed && x.Rule != "unevaluated" {
			paths = append(paths, x.Path)
		}
	}
	return strings.Join(paths, "\x00")
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
		if !slices.Contains(drop, s) {
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
	if refusedFailure(rec) {
		if life := examineTokenLifetime(e, rec); life != nil {
			return life.line()
		}
		if loss := examineSessionLoss(e, rec); loss != nil {
			return loss.line()
		}
		if fresh := repeatedFreshRefusal(e, rec); fresh != nil {
			return fresh.line()
		}
	}
	if flaky := detectIntermittent(e, rec); flaky != nil {
		return flaky.line(true)
	}
	return ""
}

func refusedFailure(rec *runner.Record) bool {
	return slices.ContainsFunc(rec.Steps, func(st *runner.StepRecord) bool {
		return st.Status != runner.StatusPassed && len(st.TokenRefused) > 0
	})
}

func quietly(fn func() error) (string, error) {
	return capturing(&os.Stdout, fn)
}

func capturing(f **os.File, fn func() error) (string, error) {
	saved := *f
	r, w, err := os.Pipe()
	if err != nil {
		return "", fn()
	}
	*f = w
	done := make(chan []byte, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- out
	}()
	runErr := fn()
	_ = w.Close()
	*f = saved
	out := <-done
	_ = r.Close()
	return string(out), runErr
}

func unappliedFilter(st *runner.StepRecord) string {
	request, response := decodedRecordStep(st)
	req, _ := request.(map[string]any)
	resp, _ := response.(map[string]any)
	names := slices.Sorted(maps.Keys(req))
	for _, name := range names {
		want, ok := req[name].(string)
		if !ok || want == "" {
			continue
		}
		for _, list := range slices.Sorted(maps.Keys(resp)) {
			items, _ := resp[list].([]any)
			asserted := slices.ContainsFunc(st.Expect, func(x chain.ExpectResult) bool {
				return !x.Passed && (x.Path == list || strings.HasPrefix(x.Path, list+"."))
			})
			if !asserted {
				continue
			}
			for _, item := range items {
				obj, _ := item.(map[string]any)
				if got, ok := obj[name].(string); ok && got != want {
					return name
				}
			}
		}
	}
	return ""
}
