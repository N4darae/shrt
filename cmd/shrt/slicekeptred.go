package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func failurePins(c *chain.Chain, rec *runner.Record, step string) ([]chain.Pin, error) {
	if rec == nil {
		return nil, fmt.Errorf("-kept-red needs a run record of the chain that reached %s: run it first", step)
	}
	var sr *runner.StepRecord
	for _, s := range rec.Steps {
		if s.ID == step {
			sr = s
		}
	}
	if sr == nil {
		return nil, fmt.Errorf("run %s did not reach step %s: nothing to pin", rec.RunID, step)
	}
	have := map[string]bool{}
	for _, k := range c.KeptRed {
		have[k.Step+"\x00"+k.Path] = true
	}
	added := []chain.Pin{}
	unevaluated := false
	for _, x := range sr.Expect {
		if x.Passed {
			continue
		}
		if x.Rule == "unevaluated" {
			unevaluated = true
			continue
		}
		if have[step+"\x00"+x.Path] {
			continue
		}
		have[step+"\x00"+x.Path] = true
		added = append(added, chain.Pin{Step: step, Path: x.Path})
	}
	if len(added) == 0 {
		why := "every expectation of it held"
		switch {
		case unevaluated || sr.Transport != nil:
			why = "its expectations were never evaluated (refused at transport, or not sent), and kept_red pins only an expectation that failed against an answer"
		case sr.Error != "":
			why = "it errored: " + sr.Error
		}
		return nil, fmt.Errorf("step %s failed no expectation in run %s (%s): nothing to pin", step, rec.RunID, why)
	}
	return added, nil
}

func failedSteps(rec *runner.Record) []string {
	out := []string{}
	for _, s := range rec.Steps {
		if s.Status == runner.StatusFailed || s.Status == runner.StatusError {
			out = append(out, s.ID)
		}
	}
	return out
}

func sliceWithout(chainArg string, drop []string, runID string, write *optionalString, name string, force, asJSON bool) error {
	writePath := ""
	bare := bareSliceFile(name)
	if bare {
		name = strings.TrimSuffix(name, ".yaml")
	} else if isSlicePath(name) {
		var err error
		if writePath, name, err = slicePathAndName(name); err != nil {
			return err
		}
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	c, err := chain.Resolve(e.chainsDir(), chainArg)
	if err != nil {
		return err
	}
	if bare {
		writePath = filepath.Join(sliceDir(e, c), name+".yaml")
	}
	ids := []string{}
	fromRun := ""
	for _, id := range drop {
		if id != "failed" {
			if !containsStr(ids, id) {
				ids = append(ids, id)
			}
			continue
		}
		if runID == "" {
			runID = "latest"
		}
		rec, err := e.store.LoadRun(c.Name, runID)
		if err != nil {
			return err
		}
		fromRun = rec.RunID
		failed := failedSteps(rec)
		if len(failed) == 0 {
			return fmt.Errorf("run %s of %s has no failed step: -without failed leaves nothing out", rec.RunID, c.Name)
		}
		for _, f := range failed {
			if _, ok := c.Step(f); ok && !containsStr(ids, f) {
				ids = append(ids, f)
			}
		}
	}
	res, err := chain.Without(c, ids, name)
	if err != nil {
		return err
	}
	written := ""
	if write.set {
		path := filepath.Join(sliceDir(e, c), res.Chain.Name+".yaml")
		if writePath != "" {
			path = writePath
		}
		source := sameSliceFile(path, c.SourcePath)
		if source {
			res.Chain.Name = c.Name
		}
		if _, err := os.Stat(path); err == nil && !force && !source {
			return fmt.Errorf("%s already exists, pass -force to overwrite it or name another file: -write <name>", path)
		}
		if err := writeSliceFile(path, res.Chain); err != nil {
			return err
		}
		written = path
	}
	if asJSON {
		return emitJSON(struct {
			*chain.WithoutResult
			Run     string `json:"run,omitempty"`
			Written string `json:"written,omitempty"`
		}{res, fromRun, written})
	}
	how := ""
	if fromRun != "" {
		how = fmt.Sprintf(" (failed in run %s)", fromRun)
	}
	fmt.Printf("%s without %s%s: %d of %d steps kept\n\n", c.Name, strings.Join(ids, ", "), how, len(res.Chain.Steps), res.Total)
	idW := 0
	for _, r := range res.Removed {
		idW = max(idW, len(r.ID))
	}
	for _, r := range res.Removed {
		fmt.Printf("  %4d  %-*s  left out: %s\n", r.Index, idW, r.ID, r.Reason)
	}
	for _, k := range res.DroppedPins {
		fmt.Printf("  kept_red pin on %s %s dropped with its step\n", k.Step, k.Path)
	}
	fmt.Printf("\nA step left in may still depend on what a left-out write did to shared state rather than on a reference: run it, " +
		"and propose it as a safe spot only once it passes. Keep the defect visible in its own chain: shrt chain slice <chain> -step <failing step> -kept-red -write <name>\n")
	if written != "" {
		fmt.Printf("\nwritten: %s\n", shownPath(written))
		fmt.Print(sweepNote(e, written, res.Chain.Name))
		return nil
	}
	raw, err := res.Chain.Marshal()
	if err != nil {
		return err
	}
	fmt.Printf("\n%s\n", string(raw))
	return nil
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
