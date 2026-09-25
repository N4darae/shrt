package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
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
	have := map[string]int{}
	for i, k := range c.KeptRed {
		have[k.Step+"\x00"+k.Path] = i
	}
	added := []chain.Pin{}
	unevaluated, refreshed := false, false
	for _, x := range sr.Expect {
		if x.Passed {
			continue
		}
		if x.Rule == "unevaluated" {
			unevaluated = true
			continue
		}
		got := stableGot(x.Path, x.Got, rec)
		if i, ok := have[step+"\x00"+x.Path]; ok {
			if i >= 0 && c.KeptRed[i].Got == nil && got != nil {
				c.KeptRed[i].Got = got
				refreshed = true
			}
			continue
		}
		have[step+"\x00"+x.Path] = -1
		added = append(added, chain.Pin{Step: step, Path: x.Path, Got: got})
	}
	if len(added) == 0 && refreshed {
		return added, nil
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

func stableGot(path string, v any, rec *runner.Record) *string {
	var text string
	switch t := v.(type) {
	case bool:
		text = fmt.Sprint(t)
	case float64:
		text = fmt.Sprint(t)
	case string:
		if t == pathmask.MaskRedacted || t == pathmask.MaskVolatile {
			return nil
		}
		for _, val := range rec.Vars {
			if sv, ok := val.(string); ok && len(sv) >= 2 && strings.Contains(t, sv) {
				return nil
			}
		}
		text = t
	default:
		return nil
	}
	if diff.LooksVolatile(path, v, v) {
		return nil
	}
	return &text
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
		if runID == "latest" && rec.ReplayOf != "" {
			if own, _ := newestRecordReaching(e, c.Name, "", "", false); own != nil {
				fmt.Fprintf(os.Stderr, "note: -run latest is run %s, the newest `shrt run` record of %s; the newest record, %s, is a `shrt verify` replay, passed over: pass -run %s to use it\n",
					own.RunID, c.Name, rec.RunID, rec.RunID)
				rec = own
			}
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
	written, replaced := "", false
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
		written, replaced = path, source
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
	fmt.Printf("%s without %s%s: the new chain holds %d of the %d steps, the %d below left out; "+
		"a count of what the file holds, not of steps known to pass: none of them has run in this shape yet\n\n",
		c.Name, strings.Join(ids, ", "), how, len(res.Chain.Steps), res.Total, len(res.Removed))
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
		if !replaced {
			fmt.Print(sweepNote(e, written, res.Chain.Name))
		}
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

type keptRedFlag struct {
	on    bool
	steps []string
}

func (f *keptRedFlag) IsBoolFlag() bool { return true }

func (f *keptRedFlag) String() string {
	if f == nil || !f.on {
		return ""
	}
	return strings.Join(f.steps, ",")
}

func (f *keptRedFlag) Set(s string) error {
	switch strings.TrimSpace(s) {
	case "true":
		f.on = true
		return nil
	case "false":
		f.on, f.steps = false, nil
		return nil
	}
	f.on = true
	for _, id := range strings.Split(s, ",") {
		if id = strings.TrimSpace(id); id != "" && !containsStr(f.steps, id) {
			f.steps = append(f.steps, id)
		}
	}
	return nil
}

func (f *keptRedFlag) arg() string {
	if !f.on {
		return ""
	}
	if len(f.steps) == 0 {
		return "-kept-red"
	}
	return "-kept-red=" + strings.Join(f.steps, ",")
}

func keptStepPins(res *chain.SliceResult, rec *runner.Record, named []string) ([]chain.Pin, error) {
	for _, id := range named {
		if id == res.Target {
			return nil, fmt.Errorf("-kept-red=%s names the target step, which -kept-red pins anyway: name only the other steps that show the defect", id)
		}
	}
	relaxable := relaxableIn(rec)
	out := []chain.Pin{}
	for _, k := range res.Kept {
		if k.ID == res.Target {
			continue
		}
		if containsStr(named, k.ID) {
			pins, err := failurePins(res.Chain, rec, k.ID)
			if err != nil {
				return nil, fmt.Errorf("-kept-red=%s: %w", k.ID, err)
			}
			out = append(out, pins...)
			continue
		}
		if !relaxable(k.ID) {
			continue
		}
		if pins, err := failurePins(res.Chain, rec, k.ID); err == nil {
			out = append(out, pins...)
		}
	}
	return out, nil
}

func pinList(c *chain.Chain, pins []chain.Pin) string {
	order := map[string]int{}
	for i, s := range c.Steps {
		order[s.ID] = i
	}
	sorted := append([]chain.Pin{}, pins...)
	sort.SliceStable(sorted, func(a, b int) bool { return order[sorted[a].Step] < order[sorted[b].Step] })
	byStep := map[string][]string{}
	steps := []string{}
	for _, p := range sorted {
		if _, seen := byStep[p.Step]; !seen {
			steps = append(steps, p.Step)
		}
		byStep[p.Step] = append(byStep[p.Step], p.Path)
	}
	parts := []string{}
	for _, s := range steps {
		parts = append(parts, s+" at "+strings.Join(byStep[s], ", "))
	}
	return strings.Join(parts, "; ")
}

func pinSubject(pins []chain.Pin) string {
	steps := map[string]bool{}
	for _, p := range pins {
		steps[p.Step] = true
	}
	if len(steps) > 1 {
		return "they"
	}
	return "it"
}
