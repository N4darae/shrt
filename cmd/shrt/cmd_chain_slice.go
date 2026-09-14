package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

func chainSlice(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("chain slice", flag.ContinueOnError)
	step := fs.String("step", "", "step id the slice must reproduce")
	mode := fs.String("mode", chain.SliceModeClosure, "closure (rebuild every producer) or pin (pin values from a run record)")
	runID := fs.String("run", "", "run record id or 'latest', required by -mode pin and -verify")
	verify := fs.Bool("verify", false, "run the slice and compare the target step's verdict against the run record")
	asJSON := fs.Bool("json", false, "emit JSON")
	vars := varFlags{}
	fs.Var(vars, "var", "override a chain var when running -verify, repeatable: -var key=value")
	write := &optionalString{}
	fs.Var(write, "write", "write the slice to .shrt/chains/<name>.yaml, defaulting to <chain>-slice-<step-id>")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) == 0 || len(rest) > 2 {
		return fmt.Errorf("usage: shrt chain slice <chain> -step <step-id> [-mode closure|pin] [-run <id>] [-write [<name>]] [-verify] [-json]")
	}
	name := write.value
	if len(rest) == 2 {
		if !write.set || name != "" {
			return fmt.Errorf("unexpected argument %q\n\nusage: shrt chain slice <chain> -step <step-id> [-write [<name>]]", rest[1])
		}
		name = rest[1]
	}
	if *step == "" {
		return fmt.Errorf("-step is required: name the step the slice must reproduce")
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	c, err := chain.Resolve(e.chainsDir(), rest[0])
	if err != nil {
		return err
	}

	opts := chain.SliceOptions{Mode: *mode, Name: name, RPCOf: rpcOf(e)}
	lib, _, libErr := e.library()
	if libErr == nil && lib != nil {
		opts.Prereqs = contract.PrereqsFor(lib)
	}
	var rec *runner.Record
	if *mode == chain.SliceModePin || *verify {
		if *runID == "" {
			return fmt.Errorf("-run <run-id|latest> is required by -mode %s and by -verify: without a run record there is nothing to pin or to compare against", *mode)
		}
		rec, err = e.store.LoadRun(c.Name, *runID)
		if err != nil {
			return err
		}
		opts.RunID = rec.RunID
		opts.Value = recordValues(rec)
	}

	res, err := chain.Slice(c, *step, opts)
	if err != nil {
		return err
	}

	written := ""
	if write.set {
		path := filepath.Join(e.chainsDir(), res.Chain.Name+".yaml")
		raw, err := res.Chain.Marshal()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			return err
		}
		written = path
	}

	var verdict *sliceVerdict
	var verifyErr error
	if *verify {
		verdict, verifyErr = runSliceVerify(ctx, e, res, rec, vars, *asJSON)
		if verdict == nil {
			return verifyErr
		}
	}

	if *asJSON {
		payload := struct {
			*chain.SliceResult
			Written    string        `json:"written,omitempty"`
			Verify     *sliceVerdict `json:"verify,omitempty"`
			Hypothesis string        `json:"hypothesis"`
		}{SliceResult: res, Written: written, Verify: verdict, Hypothesis: hypothesisLine}
		if err := emitJSON(payload); err != nil {
			return err
		}
		return verifyErr
	}

	printSlice(res, written, verdict)
	if !write.set {
		raw, err := res.Chain.Marshal()
		if err != nil {
			return err
		}
		fmt.Printf("\n%s\n", string(raw))
	}
	return verifyErr
}

const hypothesisLine = "this slice is a HYPOTHESIS until it is run: a dependency that is state rather than a reference leaves no trace in the YAML"

func printSlice(res *chain.SliceResult, written string, verdict *sliceVerdict) {
	fmt.Printf("slice of %s for step %s, mode %s", res.Source, res.Target, res.Mode)
	if res.Run != "" {
		fmt.Printf(", run %s", res.Run)
	}
	fmt.Println()
	fmt.Println()
	idW, callW := 0, 0
	for _, k := range res.Kept {
		if n := len(k.ID); n > idW {
			idW = n
		}
		if n := len(shortCall(k.Call)); n > callW {
			callW = n
		}
	}
	for _, k := range res.Kept {
		fmt.Printf("  %4d  %-*s  %-*s  %s\n", k.Index, idW, k.ID, callW, shortCall(k.Call), k.Reason)
	}
	if len(res.Pins) > 0 {
		fmt.Println("\npinned values:")
		for _, p := range res.Pins {
			fmt.Printf("  %s = %v  (was ${%s}, produced by %s)\n", p.Var, p.Value, p.Ref, p.Producer)
		}
	}
	if len(res.Unmet) > 0 {
		fmt.Println("\nunmet prerequisites, no earlier step calls them, so the slice may not stand alone:")
		for _, u := range res.Unmet {
			fmt.Printf("  %s %s (declared for %s)\n", u.Edge, u.RPC, u.Step)
		}
	}
	if res.UnderIncluded {
		scale := ""
		if len(res.Kept)*2 < res.Total {
			scale = ", under half the chain"
		}
		fmt.Printf("\nWARNING possible under-inclusion: %d of %d steps kept%s, and %d dropped step(s) WRITE.\n", len(res.Kept), res.Total, scale, len(res.DroppedWrites))
		fmt.Println("  A step that depends on state an earlier write left behind carries no reference to it, so")
		fmt.Println("  this slice can be too small and still go green. Dropped write steps:")
		dropW := 0
		for _, d := range res.DroppedWrites {
			if n := len(d.ID); n > dropW {
				dropW = n
			}
		}
		for _, d := range res.DroppedWrites {
			fmt.Printf("    %4d  %-*s  %s\n", d.Index, dropW, d.ID, shortCall(d.Call))
		}
	}
	fmt.Printf("\n%d of %d steps\n", len(res.Kept), res.Total)
	fmt.Printf("%s\n", hypothesisLine)
	if written != "" {
		fmt.Printf("wrote %s\n", written)
	}
	if verdict != nil {
		fmt.Println()
		fmt.Print(verdict.text())
	}
}

func shortCall(call string) string {
	if i := strings.LastIndex(call, "."); i >= 0 {
		return call[i+1:]
	}
	return call
}

func rpcOf(e *env) func(*chain.Step) string {
	return func(s *chain.Step) string {
		m, err := e.cat.Lookup(s.Call)
		if err != nil {
			return ""
		}
		return m.FullName
	}
}

func recordValues(rec *runner.Record) func(string) (any, bool) {
	scope := chain.NewScope(rec.Vars)
	for k, v := range rec.Exports {
		scope.Exports[k] = v
	}
	for _, s := range rec.Steps {
		var request, response any
		if len(s.Request) > 0 {
			_ = json.Unmarshal(s.Request, &request)
		}
		if len(s.Response) > 0 {
			_ = json.Unmarshal(s.Response, &response)
		}
		scope.Record(s.ID, request, response)
	}
	return func(ref string) (any, bool) {
		v, err := scope.ResolveValue("${" + ref + "}")
		if err != nil {
			return nil, false
		}
		if text, ok := v.(string); ok && text == pathmask.MaskRedacted {
			return nil, false
		}
		return v, true
	}
}

const (
	sliceReproduced    = "reproduced"
	sliceNotReproduced = "not_reproduced"
	sliceInconclusive  = "inconclusive"
	sliceDidNotRun     = "did_not_run"
)

type sliceVerdict struct {
	Step         string        `json:"step"`
	Outcome      string        `json:"outcome"`
	Reproduced   bool          `json:"reproduced"`
	Reason       string        `json:"reason,omitempty"`
	EnvelopePath string        `json:"envelope_path"`
	SourceRun    string        `json:"source_run"`
	SliceRun     string        `json:"slice_run,omitempty"`
	SliceRecord  string        `json:"slice_record,omitempty"`
	Status       string        `json:"slice_status,omitempty"`
	Source       chain.Verdict `json:"source"`
	Replay       chain.Verdict `json:"replay"`
	Differences  []string      `json:"differences,omitempty"`
}

func (v *sliceVerdict) text() string {
	var b strings.Builder
	head := map[string]string{
		sliceReproduced:    "reproduced",
		sliceNotReproduced: "NOT REPRODUCED",
		sliceInconclusive:  "INCONCLUSIVE",
		sliceDidNotRun:     "DID NOT RUN",
	}[v.Outcome]
	slice := v.SliceRun
	if slice == "" {
		slice = "none"
	}
	fmt.Fprintf(&b, "verify %s: step %s, source run %s, slice run %s\n", head, v.Step, v.SourceRun, slice)
	fmt.Fprintf(&b, "  source: status %s, %s %q\n", v.Source.Status, v.EnvelopePath, v.Source.ErrorCode)
	if v.Outcome == sliceDidNotRun {
		fmt.Fprintf(&b, "  slice:  step %s was never sent, so there is no verdict to compare\n", v.Step)
	} else {
		fmt.Fprintf(&b, "  slice:  status %s, %s %q\n", v.Replay.Status, v.EnvelopePath, v.Replay.ErrorCode)
	}
	for _, d := range v.Differences {
		fmt.Fprintf(&b, "  %s\n", d)
	}
	if v.Reason != "" {
		fmt.Fprintf(&b, "  %s\n", strings.ReplaceAll(v.Reason, "\n", "\n  "))
	}
	if v.SliceRecord != "" {
		fmt.Fprintf(&b, "  record %s\n", v.SliceRecord)
	}
	return b.String()
}

func (v *sliceVerdict) err() error {
	switch v.Outcome {
	case sliceNotReproduced:
		return exitWith(1, "step %s was NOT reproduced by the slice", v.Step)
	case sliceDidNotRun:
		return exitWith(2, "the slice DID NOT RUN step %s, so nothing was verified", v.Step)
	case sliceInconclusive:
		return exitWith(3, "step %s: INCONCLUSIVE, the verdict matched but the slice dropped write step(s)", v.Step)
	}
	return nil
}

func runSliceVerify(ctx context.Context, e *env, res *chain.SliceResult, rec *runner.Record, vars varFlags, quiet bool) (*sliceVerdict, error) {
	source, ok := rec.Step(res.Target)
	if !ok {
		return nil, fmt.Errorf("run %s has no step %q, so there is no verdict to reproduce", rec.RunID, res.Target)
	}
	v := &sliceVerdict{
		Step:         res.Target,
		EnvelopePath: chain.EnvelopePath(),
		SourceRun:    rec.RunID,
		Source:       verdictOf(source),
	}
	didNotRun := func(reason string) (*sliceVerdict, error) {
		v.Outcome = sliceDidNotRun
		v.Reason = reason
		return v, v.err()
	}
	replayRec, err := executeChain(ctx, e, res.Chain, runner.Options{
		Vars: vars, Volatile: e.cfg.Volatile, Redact: e.cfg.Redact,
	}, quiet)
	if err != nil {
		return didNotRun("could not run the slice: " + err.Error())
	}
	v.SliceRun = replayRec.RunID
	v.Status = replayRec.Status
	if path, saveErr := e.store.SaveRun(replayRec); saveErr == nil {
		v.SliceRecord = path
	}
	replay, ok := replayRec.Step(res.Target)
	if !ok {
		return didNotRun(fmt.Sprintf("the slice run stopped before step %q (%s): %s",
			res.Target, replayRec.Status, replayRec.Failure))
	}
	if replay.Status == runner.StatusError || replay.Status == runner.StatusSkipped {
		return didNotRun(fmt.Sprintf("step %q was never sent (%s): %s", res.Target, replay.Status, replay.Error))
	}
	if replay.Transport != nil {
		return didNotRun(fmt.Sprintf("step %q never reached the backend (%s: %s)",
			res.Target, replay.Transport.Code, replay.Transport.Message))
	}
	v.Replay = verdictOf(replay)
	v.Differences = chain.CompareVerdicts(v.Source, v.Replay)
	switch {
	case len(v.Differences) > 0:
		v.Outcome = sliceNotReproduced
	case res.UnderIncluded:
		v.Outcome = sliceInconclusive
		names := make([]string, 0, len(res.DroppedWrites))
		for _, d := range res.DroppedWrites {
			names = append(names, d.ID)
		}
		v.Reason = fmt.Sprintf("the verdict matched, but the slice dropped %d write step(s): %s.\n"+
			"A match can come from state the slice never built, so it is not evidence that the slice reproduces\n"+
			"the failure. Put the writes the target may depend on back into the written slice, run it, and\n"+
			"compare that run's step %s with the source run.", len(names), strings.Join(names, ", "), res.Target)
	default:
		v.Outcome = sliceReproduced
		v.Reproduced = true
	}
	return v, v.err()
}

func verdictOf(sr *runner.StepRecord) chain.Verdict {
	var response any
	if len(sr.Response) > 0 {
		_ = json.Unmarshal(sr.Response, &response)
	}
	code := ""
	if v, ok := chain.Get(response, chain.EnvelopePath()); ok {
		code = fmt.Sprintf("%v", v)
	}
	return chain.Verdict{Step: sr.ID, Status: sr.Status, ErrorCode: code, Expect: sr.Expect}
}
