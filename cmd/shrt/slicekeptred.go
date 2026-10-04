package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

func failurePins(c *chain.Chain, rec *runner.Record, step string) ([]chain.Pin, error) {
	if rec == nil {
		return nil, fmt.Errorf("pinning needs a run record of the chain that reached %s: run it first", step)
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
		got := stableGot(x.Path, x.Got, rec, c, step)
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

func stableGot(path string, v any, rec *runner.Record, c *chain.Chain, step string) *string {
	var text string
	switch t := v.(type) {
	case nil:
		text = ""
		return &text
	case bool:
		text = fmt.Sprint(t)
	case float64:
		text = fmt.Sprint(t)
	case string:
		if t == pathmask.MaskRedacted || t == pathmask.MaskVolatile {
			return nil
		}
		text = t
	default:
		return nil
	}
	best := ""
	for k, val := range rec.Vars {
		if sv, ok := val.(string); ok && len(sv) >= 2 && strings.Contains(text, sv) && (best == "" || len(sv) > len(rec.Vars[best].(string))) {
			best = k
		}
	}
	volatile := diff.LooksVolatile(path, v, v)
	if best == "" && !volatile {
		return &text
	}
	if ref := producedBy(text, rec, c, step); ref != "" {
		return &ref
	}
	if best == "" {
		return nil
	}
	text = strings.ReplaceAll(text, rec.Vars[best].(string), "${vars."+best+"}")
	return &text
}

func producedBy(text string, rec *runner.Record, c *chain.Chain, step string) string {
	if len(text) < 4 {
		return ""
	}
	for _, st := range rec.Steps {
		if st.ID == step {
			break
		}
		if _, kept := c.Step(st.ID); !kept {
			continue
		}
		var body any
		if json.Unmarshal(st.Response, &body) != nil {
			continue
		}
		found := ""
		eachLeaf(body, "", func(p string, v any) {
			if s, ok := v.(string); ok && s == text && (found == "" || len(p) < len(found)) {
				found = p
			}
		})
		if found != "" {
			return "${" + st.ID + "." + found + "}"
		}
	}
	return ""
}

func failedSteps(rec *runner.Record) []string {
	out := []string{}
	for _, s := range rec.Steps {
		if failing(s) {
			out = append(out, s.ID)
		}
	}
	return out
}

func sliceWithout(ctx context.Context, chainArg string, drop []string, runID string, write *optionalString, name string, asJSON bool, verify *withoutVerify) error {
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
		writePath = slicePath(e, c, name+".yaml", false)
	}
	if verify != nil {
		verify.ref = sliceChainRef(chainArg, c)
	}
	ids := []string{}
	fromRun := ""
	var rec *runner.Record
	load := func() error {
		if rec != nil {
			return nil
		}
		if runID == "" || runID == "latest" {
			rec, err = latestRun(e, c.Name, "")
		} else {
			rec, err = e.store.LoadRun(c.Name, runID)
		}
		return err
	}
	if verify != nil {
		if err := load(); err != nil {
			return err
		}
	}
	for _, id := range drop {
		if id != "failed" {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
			continue
		}
		if err := load(); err != nil {
			return err
		}
		fromRun = rec.RunID
		failed := failedSteps(rec)
		if len(failed) == 0 {
			return fmt.Errorf("run %s of %s has no failed step: -without failed leaves nothing out", rec.RunID, c.Name)
		}
		for _, f := range failed {
			if _, ok := c.Step(f); ok && !slices.Contains(ids, f) {
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
		path := slicePath(e, c, res.Chain.Name+".yaml", false)
		if writePath != "" {
			path = writePath
		}
		source := sameFile(path, c.SourcePath)
		if source {
			res.Chain.Name = c.Name
		}
		if _, err := os.Stat(path); err == nil && !source {
			return fmt.Errorf("%s already exists: name another file, -write <name>", path)
		}
		if err := writeWithout(path, source, res); err != nil {
			return err
		}
		written, replaced = path, source
	}
	var verdict *withoutVerdict
	if asJSON {
		if verify != nil {
			if verdict, err = verifyWithout(ctx, e, res, rec, ids, verify, written != "", true); err != nil {
				return err
			}
		}
		if err := emitJSON(struct {
			*chain.WithoutResult
			Run     string          `json:"run,omitempty"`
			Written string          `json:"written,omitempty"`
			Verify  *withoutVerdict `json:"verify,omitempty"`
		}{res, fromRun, written, verdict}); err != nil {
			return err
		}
		return verdict.err()
	}
	how := ""
	if fromRun != "" {
		how = fmt.Sprintf(" (failed in run %s)", fromRun)
	}
	fmt.Printf("%s without %s%s: the new chain holds %d of the %d steps, the %d below left out\n\n",
		c.Name, capList(ids, 3), how, len(res.Chain.Steps), res.Total, len(res.Removed))
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
	if verify != nil {
		if verdict, err = verifyWithout(ctx, e, res, rec, ids, verify, written != "", false); err != nil {
			return err
		}
		fmt.Print("\n" + verdict.text())
	} else {
		fmt.Println("\nunverified: run it before proposing it (-verify), or keep the defect red instead: shrt chain pin " + sliceChainRef(chainArg, c))
	}
	if written != "" {
		fmt.Printf("\nwritten: %s\n", shownPath(written))
		if !replaced {
			fmt.Print(sweepNote(e, written))
		}
		return verdict.err()
	}
	if verdict != nil {
		return verdict.err()
	}
	raw, err := res.Chain.Marshal()
	if err != nil {
		return err
	}
	fmt.Printf("\n%s\n", string(raw))
	return nil
}

func writeWithout(path string, source bool, res *chain.WithoutResult) error {
	if source {
		if raw, err := os.ReadFile(path); err == nil {
			if edited, ok := res.EditSource(raw, path); ok {
				return os.WriteFile(path, edited, 0o644)
			}
		}
	}
	return writeSliceFile(path, res.Chain)
}

type sliceKeptRed struct {
	on      bool
	steps   []string
	verdict *sliceVerdict
}

func keptStepPins(res *chain.SliceResult, rec *runner.Record, named []string) ([]chain.Pin, error) {
	relaxable := relaxableIn(rec)
	out := []chain.Pin{}
	for _, k := range res.Kept {
		if k.ID == res.Target {
			continue
		}
		if slices.Contains(named, k.ID) {
			pins, err := failurePins(res.Chain, rec, k.ID)
			if err != nil {
				return nil, fmt.Errorf("pin %s: %w", k.ID, err)
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

func replayPins(res *chain.SliceResult, replay *runner.Record) []chain.Pin {
	ids := []string{}
	for _, k := range res.Kept {
		if k.ID != res.Target {
			ids = append(ids, k.ID)
		}
	}
	out := []chain.Pin{}
	for _, id := range append(ids, res.Target) {
		if pins, err := failurePins(res.Chain, replay, id); err == nil {
			out = append(out, pins...)
		}
	}
	return out
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

func checkpointReads(c *chain.Chain, res *chain.SliceResult, rec *runner.Record, targets []string) map[string]string {
	kept := map[string]bool{}
	for _, k := range res.Kept {
		kept[k.ID] = true
	}
	out := map[string]string{}
	for _, target := range targets {
		at := slices.IndexFunc(c.Steps, func(s *chain.Step) bool { return s.ID == target })
		src, ok := rec.Step(target)
		if at < 0 || !ok || !chain.IsReadOnlyCall(c.Steps[at].Call) {
			continue
		}
		records := requestIDs(src)
		if len(records) == 0 {
			continue
		}
		for _, x := range src.Expect {
			if x.Passed || x.Rule == "unevaluated" || x.Path == chain.EnvelopePath() || strings.HasPrefix(x.Path, "transport.") {
				continue
			}
			if id, why := checkpointFor(c, rec, kept, at, records, x.Path); id != "" && !kept[id] {
				out[id] = why
			}
		}
	}
	return out
}

func checkpointFor(c *chain.Chain, rec *runner.Record, kept map[string]bool, at int, records map[string]bool, path string) (string, string) {
	field := chain.PathLeaf(path)
	for j := at - 1; j >= 0; j-- {
		s := c.Steps[j]
		sr, ok := rec.Step(s.ID)
		if !ok || !chain.IsReadOnlyCall(s.Call) || !maps.Equal(requestIDs(sr), records) {
			continue
		}
		if sr.Status != runner.StatusPassed || !slices.ContainsFunc(sr.Expect, func(x chain.ExpectResult) bool { return chain.PathLeaf(x.Path) == field }) {
			continue
		}
		writes := []string{}
		for _, w := range c.Steps[j+1 : at] {
			if !kept[w.ID] || chain.IsReadOnlyCall(w.Call) {
				continue
			}
			if overlaps(entityFactsWith(rec, w.ID, true).acts, records) {
				writes = append(writes, w.ID)
			}
		}
		if len(writes) == 0 {
			return "", ""
		}
		return s.ID, fmt.Sprintf("%s: last passing read of %s before %s", chain.KeepCheckpoint, path, strings.Join(writes, ", "))
	}
	return "", ""
}

func requestIDs(sr *runner.StepRecord) map[string]bool {
	request, _ := decodedRecordStep(sr)
	out := map[string]bool{}
	collectIDs(request, out)
	return out
}
