package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"gopkg.in/yaml.v3"
)

type sliceProgress struct{ verify, sent bool }

const sliceUsage = "usage: shrt chain slice <chain> -step <step-id> [-mode closure|pin] [-run <id>] [-keep <id,...>] [-var k=v] [-write [<name>] [-force]] [-verify [-build <id>]] [-json]"

const sliceExitCodes = "\nexit codes, plain slice (no -verify):\n" +
	"  0  the slice was printed or written\n" +
	"  1  a refusal: an unknown chain or step, -mode pin without -run, an unknown run, a slice file\n" +
	"     -write would overwrite; the same refusal exits 2 under -verify, where 1 means NOT REPRODUCED\n" +
	"\nexit codes with -verify:\n" +
	"  0  reproduced\n" +
	"  1  NOT REPRODUCED; a flag that cannot be parsed exits 1 in either mode\n" +
	"  2  DID NOT RUN: the target step was never answered, or -verify refused before\n" +
	"     anything was sent (an unknown chain or step, no -run, a run that does not reach the step,\n" +
	"     a missing or not-fresh -var name=<fresh>), so nothing was verified\n" +
	"  3  INCONCLUSIVE: the verdict matched but the slice dropped write step(s), or the source run was\n" +
	"     recorded against another target than the config's (under -mode pin nothing is sent then)\n"

func chainSlice(ctx context.Context, args []string) error {
	p := &sliceProgress{}
	err := sliceChain(ctx, args, p)
	var coded *exitError
	if err == nil || !p.verify || p.sent || errors.Is(err, flag.ErrHelp) || errors.As(err, &coded) {
		return err
	}
	return &exitError{code: 2, err: err}
}

func sliceChain(ctx context.Context, args []string, p *sliceProgress) error {
	fs := flag.NewFlagSet("chain slice", flag.ContinueOnError)
	step := fs.String("step", "", "step id the slice must reproduce")
	mode := fs.String("mode", chain.SliceModeClosure, "closure (rebuild every producer) or pin (pin values from a run record)")
	runID := fs.String("run", "", "run record id or 'latest', required by -mode pin and -verify")
	verify := fs.Bool("verify", false, "run the slice and compare the target step's verdict against the run record")
	asJSON := fs.Bool("json", false, "emit JSON")
	build := fs.String("build", "", "with -verify, "+buildFlagUsage+"; the verdict names the build the slice run held on")
	force := fs.Bool("force", false, "with -write, overwrite an existing chain file that is not this command's slice of the same step, or a VERIFIED slice this one differs from")
	vars := varFlags{}
	fs.Var(vars, "var", "set a var, repeatable: -var key=value; overrides a chain var when running -verify, and a var the chain does not declare is written into the slice")
	write := &optionalString{}
	fs.Var(write, "write", "write the slice to .shrt/chains/<name>.yaml; `[name]` is optional (-write, or -write <name>) and defaults to <chain>-slice-<step-id>")
	keep := &stepList{}
	fs.Var(keep, "keep", "also keep these earlier steps and what they need, comma-separated or repeated: -keep `id[,id]`; the word writes keeps every earlier write step, and combines with ids: -keep writes,<id>")
	setUsage(fs, sliceUsage, sliceExitCodes)
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	p.verify = *verify
	if len(rest) == 0 || len(rest) > 2 {
		return errors.New(sliceUsage)
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

	opts := chain.SliceOptions{Mode: *mode, Name: name, RPCOf: rpcOf(e), Keep: *keep, Vars: vars, IsLogin: isLoginStep(e)}
	lib, err := e.library()
	if err != nil {
		return err
	}
	opts.Prereqs = contract.PrereqsFor(lib)
	var rec *runner.Record
	if *runID == "" {
		if err := runRequired(*mode, *verify); err != nil {
			return err
		}
	}
	if *runID != "" {
		needStep := *mode == chain.SliceModePin || *verify
		if _, known := c.Step(*step); !known {
			needStep = false
		}
		if needStep {
			rec, err = loadRunReaching(e, c.Name, *runID, *step)
		} else {
			rec, err = e.store.LoadRun(c.Name, *runID)
		}
		if err != nil {
			return err
		}
		if err := foreignSourceRun(e, rec, *mode, *verify); err != nil {
			return err
		}
		opts.RunID = rec.RunID
		opts.Value = recordValues(rec)
		opts.RunVars = recordVars(rec)
		opts.RunVarsAsDefaults = *verify
		opts.Refused = refusedIn(rec)
		opts.Performed = performedIn(rec)
	}

	res, err := chain.Slice(c, *step, opts)
	if err != nil {
		return err
	}
	if *verify && len(res.MissingVars) > 0 {
		return missingVarsError(res, rec)
	}
	if *verify {
		if err := freshVarsError(res, c, rec, vars); err != nil {
			return err
		}
	}

	whole := write.set && name == "" && len(res.Kept) == res.Total && c.SourcePath != ""
	if whole {
		res.Chain.Name = c.Name
	}
	written := ""
	if whole {
		if !*asJSON {
			fmt.Printf("the slice keeps all %d steps of %s, so it is %s itself: no %s.yaml written",
				res.Total, c.Name, c.Name, chain.DefaultSliceName(c.Name, *step))
			if *verify {
				fmt.Printf(", and a reproduced verdict is recorded in %s", c.SourcePath)
			}
			fmt.Println()
		}
	} else if write.set {
		path := filepath.Join(e.chainsDir(), res.Chain.Name+".yaml")
		if err := mayOverwriteSlice(path, res, *force, *verify); err != nil {
			return err
		}
		if err := writeSliceFile(path, res.Chain); err != nil {
			return err
		}
		written = path
	}

	var verdict *sliceVerdict
	var verifyErr error
	if *verify {
		if !*asJSON {
			printSliceHeader(res)
		}
		verdict, verifyErr = runSliceVerify(ctx, e, res, rec, sliceVerifyArgs{
			vars: vars, quiet: *asJSON, persist: write.set, name: name, keep: *keep, build: *build,
			otherTarget: sourceTargetDiffers(e, rec),
			reslice: func(keep []string) *chain.SliceResult {
				o := opts
				o.Keep = keep
				next, err := chain.Slice(c, *step, o)
				if err != nil {
					return nil
				}
				return next
			},
		})
		if verdict == nil {
			return verifyErr
		}
		p.sent = true
		res.Build = verdict.Build
		now := time.Now()
		switch {
		case verdict.Outcome == sliceReproduced && whole:
			res.Verified = res.OwnRunVerdict(c.Name, verdict.SourceRun, verdict.SliceRun, now)
			record := chain.RecordVerified
			if chain.IsSliceDescription(c.Description) {
				record = chain.RecordRerun
			}
			if err := recordVerdictIn(c.SourcePath, func(d string) string { return record(d, res.Verified) }); err != nil {
				return err
			}
			verdict.Recorded = c.SourcePath
		case verdict.Outcome == sliceReproduced:
			res.MarkReproduced(verdict.SourceRun, verdict.SliceRun, now)
		case verdict.Outcome == sliceNotReproduced && !whole:
			first := ""
			if len(verdict.Differences) > 0 {
				first = verdict.Differences[0]
			}
			res.MarkNotReproduced(verdict.SourceRun, verdict.SliceRun, now, first)
		}
		if (verdict.Outcome == sliceReproduced || verdict.Outcome == sliceNotReproduced) && written != "" {
			if err := writeSliceFile(written, res.Chain); err != nil {
				return err
			}
			verdict.Recorded = written
		}
	}

	if *asJSON {
		payload := struct {
			*chain.SliceResult
			Written    string        `json:"written,omitempty"`
			Verify     *sliceVerdict `json:"verify,omitempty"`
			Hypothesis string        `json:"hypothesis,omitempty"`
		}{SliceResult: res, Written: written, Verify: verdict}
		if !verdict.settled() {
			payload.Hypothesis = hypothesisLine
		}
		if err := emitJSON(payload); err != nil {
			return err
		}
		return verifyErr
	}

	if !*verify {
		printSliceHeader(res)
	}
	printSlice(res, written, verdict)
	if written != "" {
		fmt.Print(sweepNote(rel(e.cfg.Root, written), res.Chain.Name))
	}
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

func printSliceHeader(res *chain.SliceResult) {
	fmt.Printf("slice of %s for step %s, mode %s", res.Source, res.Target, res.Mode)
	if res.Run != "" {
		fmt.Printf(", run %s", res.Run)
	}
	fmt.Println()
	fmt.Println()
}

func printSlice(res *chain.SliceResult, written string, verdict *sliceVerdict) {
	if verdict != nil {
		fmt.Println()
	}
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
	undeclared, fromRun := []chain.FilledVar{}, []chain.FilledVar{}
	for _, f := range res.FilledVars {
		if f.Declared {
			fromRun = append(fromRun, f)
		} else {
			undeclared = append(undeclared, f)
		}
	}
	if len(undeclared) > 0 {
		fmt.Println("\nvars the chain does not declare, written into the slice:")
		for _, f := range undeclared {
			from := "from -var"
			if f.From == chain.VarFromRun {
				from = "the value run " + res.Run + " used"
			}
			fmt.Printf("  %s = %v  (%s)\n", f.Var, f.Value, from)
		}
	}
	if len(fromRun) > 0 {
		fmt.Printf("\nvars written with the value run %s used, not the chain's default, so the slice sends what that run sent:\n", res.Run)
		for _, f := range fromRun {
			fmt.Printf("  %s = %v  (default %v)\n", f.Var, f.Value, f.Default)
		}
	}
	if len(res.FreshVars) > 0 {
		flags := make([]string, 0, len(res.FreshVars))
		for _, name := range res.FreshVars {
			flags = append(flags, "-var "+name+"=<fresh>")
		}
		fmt.Printf("\nkept write steps interpolate %s into what they create, so every run of this slice needs a value\n"+
			"this backend has not seen: %s\n", strings.Join(res.FreshVars, ", "), strings.Join(flags, " "))
	}
	if len(res.MissingVars) > 0 {
		fmt.Println("\nvars the kept steps read that the chain does not declare and nothing supplied; the slice")
		fmt.Printf("cannot run without them, pass: %s\n", strings.Join(missingVarFlags(res, nil), " "))
	}
	if len(res.Satisfied) > 0 {
		fmt.Printf("\ncontract prerequisites run %s already performed, left to that run and not re-sent (pin mode reproduces\n"+
			"the state that run left; re-sending a write would change it):\n", res.Run)
		for _, sat := range res.Satisfied {
			fmt.Printf("  %4d  %s  %s %s (declared for %s)\n", sat.Index, sat.ID, sat.Edge, sat.RPC, sat.For)
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
		if len(res.Kept)*2 < res.Reach {
			scale = ", under half of them"
		}
		fmt.Printf("\nWARNING possible under-inclusion: %d of the %d steps up to the target kept%s, and %d dropped step(s) WRITE.\n", len(res.Kept), res.Reach, scale, len(res.DroppedWrites))
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
	if len(res.RefusedWrites) > 0 {
		fmt.Printf("\n%d dropped write step(s) wrote nothing in run %s, so they do not count toward under-inclusion:\n", len(res.RefusedWrites), res.Run)
		dropW, callW := 0, 0
		for _, d := range res.RefusedWrites {
			if n := len(d.ID); n > dropW {
				dropW = n
			}
			if n := len(shortCall(d.Call)); n > callW {
				callW = n
			}
		}
		for _, d := range res.RefusedWrites {
			fmt.Printf("    %4d  %-*s  %-*s  %s\n", d.Index, dropW, d.ID, callW, shortCall(d.Call), d.Reason)
		}
	}
	fmt.Printf("\n%d of %d steps\n", len(res.Kept), res.Total)
	if !verdict.settled() {
		fmt.Printf("%s\n", hypothesisLine)
	}
	if written != "" {
		fmt.Printf("wrote %s\n", written)
	}
	if verdict != nil {
		fmt.Println()
		fmt.Print(verdict.text())
	}
}

func sweepNote(shown, name string) string {
	return fmt.Sprintf("note: %s is now part of every sweep, like any chain there: `shrt chain lint` and `chain hollow` read it, "+
		"and a gate that runs every .shrt/chains/*.yaml (README) runs it. To keep an exploratory slice out, move it:\n"+
		"  mkdir -p .shrt/scratch && mv %s .shrt/scratch/\n"+
		"  shrt run .shrt/scratch/%s.yaml still runs it by path\n", shown, shown, name)
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

func isLoginStep(e *env) func(*chain.Step) bool {
	logins := map[string]bool{}
	for _, p := range e.cfg.AuthProfiles() {
		if p == nil || p.Call == "" {
			continue
		}
		logins[p.Call] = true
		if m, err := e.cat.Lookup(p.Call); err == nil {
			logins[m.FullName] = true
		}
	}
	rpc := rpcOf(e)
	return func(s *chain.Step) bool {
		if len(logins) == 0 {
			return false
		}
		return logins[s.Call] || logins[rpc(s)]
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
	Build        string        `json:"build,omitempty"`
	SliceRecord  string        `json:"slice_record,omitempty"`
	Status       string        `json:"slice_status,omitempty"`
	Source       chain.Verdict `json:"source"`
	Replay       chain.Verdict `json:"replay"`
	Differences  []string      `json:"differences,omitempty"`
	Next         string        `json:"next,omitempty"`
	NotKeepable  []string      `json:"not_keepable,omitempty"`
	OtherTarget  string        `json:"source_target_differs,omitempty"`
	Recorded     string        `json:"verdict_written_to,omitempty"`
}

type stepList []string

func (l *stepList) String() string { return strings.Join(*l, ",") }

func (l *stepList) Set(s string) error {
	for _, id := range strings.Split(s, ",") {
		if id = strings.TrimSpace(id); id != "" {
			*l = append(*l, id)
		}
	}
	return nil
}

type sliceVerifyArgs struct {
	vars    varFlags
	quiet   bool
	persist bool
	name    string
	keep    []string
	build   string
	reslice func(keep []string) *chain.SliceResult

	otherTarget string
}

func sourceTargetDiffers(e *env, rec *runner.Record) string {
	if rec == nil || !e.otherTarget(rec.Target) {
		return ""
	}
	return fmt.Sprintf("the source run was recorded against %s, this target is %s", rec.Target, e.targetURL())
}

func foreignSourceRun(e *env, rec *runner.Record, mode string, verify bool) error {
	where := sourceTargetDiffers(e, rec)
	if where == "" || mode != chain.SliceModePin {
		return nil
	}
	msg := fmt.Sprintf("%s: -mode pin would send the ids and values run %s was given there, which this target never issued,\n"+
		"so its answer (a not-found) would say nothing about step behaviour. Slice with -mode closure, which rebuilds every\n"+
		"producer on this target, or run the chain here (shrt run %s) and pin from that run: -run latest", where, rec.RunID, rec.Chain)
	if verify {
		return exitWith(3, "INCONCLUSIVE, nothing was sent: %s", msg)
	}
	return errors.New(msg)
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
	fmt.Fprintf(&b, "verify %s: step %s, source run %s, slice run %s", head, v.Step, v.SourceRun, slice)
	if v.Build != "" {
		fmt.Fprintf(&b, " on build %s", v.Build)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  source: status %s, %s %q%s\n", v.Source.Status, v.EnvelopePath, v.Source.ErrorCode, refusalText(v.Source))
	if v.Outcome == sliceDidNotRun {
		fmt.Fprintf(&b, "  slice:  step %s was never sent, so there is no verdict to compare\n", v.Step)
	} else {
		fmt.Fprintf(&b, "  slice:  status %s, %s %q%s\n", v.Replay.Status, v.EnvelopePath, v.Replay.ErrorCode, refusalText(v.Replay))
		for _, line := range failedExpectLines(v.Source, v.Replay) {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	for _, d := range v.Differences {
		fmt.Fprintf(&b, "  %s\n", d)
	}
	if v.Reason != "" {
		fmt.Fprintf(&b, "  %s\n", strings.ReplaceAll(v.Reason, "\n", "\n  "))
	}
	if v.SliceRecord != "" {
		fmt.Fprintf(&b, "  record %s\n", v.SliceRecord)
	} else if v.SliceRun != "" {
		fmt.Fprintf(&b, "  slice run not kept: the slice was not written, so its record would name a chain that does not exist.\n"+
			"  Add -write to keep the slice and its run record.\n")
	}
	if v.Recorded != "" {
		fmt.Fprintf(&b, "  verdict recorded in the description of %s\n", v.Recorded)
	}
	if v.Next != "" {
		fmt.Fprintf(&b, "  next: %s\n", v.Next)
		if strings.Contains(v.Next, "=<fresh>") {
			b.WriteString("  replace each <fresh> with a value this backend has not seen: the var is interpolated into\n" +
				"  names the slice creates, and the run above already used its old value.\n")
		}
	}
	return b.String()
}

func failedExpectLines(source, replay chain.Verdict) []string {
	out := []string{}
	used := map[int]bool{}
	for _, e := range source.Expect {
		if e.Passed {
			continue
		}
		got := "not evaluated"
		for i, r := range replay.Expect {
			if !used[i] && r.Path == e.Path && r.Rule == e.Rule {
				used[i] = true
				got = "got=" + quoted(r.Got)
				if r.Passed {
					got += " (held)"
				}
				break
			}
		}
		out = append(out, fmt.Sprintf("failed: %s %s want=%s source got=%s, slice %s", e.Path, e.Rule, quoted(e.Want), quoted(e.Got), got))
	}
	for i, r := range replay.Expect {
		if !r.Passed && !used[i] {
			out = append(out, fmt.Sprintf("failed in the slice only: %s %s want=%s got=%s", r.Path, r.Rule, quoted(r.Want), quoted(r.Got)))
		}
	}
	return out
}

func quoted(v any) string {
	if text, ok := v.(string); ok && (text == "" || strings.TrimSpace(text) != text) {
		return strconv.Quote(text)
	}
	if v == nil {
		return "(absent)"
	}
	return fmt.Sprint(v)
}

func refusalText(v chain.Verdict) string {
	out := ""
	if m := v.Refusal["message"]; m != "" {
		out += " message=" + strconv.Quote(m)
	}
	for _, field := range []string{"reason", "app_code"} {
		if s := v.Refusal[field]; s != "" {
			out += " " + field + "=" + s
		}
	}
	if v.Transport != "" {
		out += ", transport " + v.Transport
	}
	return out
}

func (v *sliceVerdict) settled() bool {
	return v != nil && v.Outcome != sliceDidNotRun
}

func (v *sliceVerdict) err() error {
	switch v.Outcome {
	case sliceNotReproduced:
		return exitWith(1, "step %s was NOT reproduced by the slice", v.Step)
	case sliceDidNotRun:
		return exitWith(2, "the slice DID NOT RUN step %s, so nothing was verified", v.Step)
	case sliceInconclusive:
		if v.OtherTarget != "" {
			return exitWith(3, "step %s: INCONCLUSIVE, %s", v.Step, v.OtherTarget)
		}
		return exitWith(3, "step %s: INCONCLUSIVE, the verdict matched but the slice dropped write step(s)", v.Step)
	}
	return nil
}

func runSliceVerify(ctx context.Context, e *env, res *chain.SliceResult, rec *runner.Record, a sliceVerifyArgs) (*sliceVerdict, error) {
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
		Vars: a.vars, Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, Build: a.build,
	}, a.quiet)
	if err != nil {
		return didNotRun("could not run the slice: " + err.Error())
	}
	v.SliceRun = replayRec.RunID
	v.Build = replayRec.Build
	v.Status = replayRec.Status
	if a.persist {
		if path, saveErr := e.store.SaveRun(replayRec); saveErr == nil {
			v.SliceRecord = path
		}
	}
	replay, ok := replayRec.Step(res.Target)
	if !ok {
		return didNotRun(fmt.Sprintf("the slice run stopped before step %q (%s): %s",
			res.Target, replayRec.Status, replayRec.Failure))
	}
	answered := replay.HTTPStatus != 0 || len(replay.Response) > 0
	if replay.Status == runner.StatusSkipped || (replay.Status == runner.StatusError && !answered) {
		return didNotRun(fmt.Sprintf("step %q was never sent (%s): %s", res.Target, replay.Status, replay.Error))
	}
	if replay.Transport != nil && !answered {
		return didNotRun(fmt.Sprintf("step %q never reached the backend (%s: %s)",
			res.Target, replay.Transport.Code, replay.Transport.Message))
	}
	v.Replay = verdictOf(replay)
	v.Differences = chain.CompareVerdictsMasking(v.Source, v.Replay, sameUpToIDs)
	switch {
	case len(v.Differences) > 0 && a.otherTarget != "":
		v.Outcome = sliceInconclusive
		v.OtherTarget = a.otherTarget
		v.Reason = a.otherTarget + ": the verdict differs, and the difference can come from the target (its data, build\n" +
			"or configuration) rather than from what the slice left out. Run the chain on this target (shrt run " + rec.Chain + ")\n" +
			"and verify the slice against that run: -run latest"
	case len(v.Differences) > 0:
		v.Outcome = sliceNotReproduced
		if res.UnderIncluded {
			names := droppedNames(res)
			v.Reason = fmt.Sprintf("the slice dropped %d write step(s): %s.\n"+
				"The difference can come from state those writes would have built. Keep them and verify again\n"+
				"against the same source run; if the verdict then matches, drop ids from -keep to find the one\n"+
				"the target needs.", len(names), strings.Join(names, ", "))
			v.suggestKeep(res, rec, a, names)
		}
		if differ := varsDifferBetween(rec, replayRec, freshSet(res)); differ != "" {
			v.Reason = strings.TrimPrefix(v.Reason+"\nthe slice ran with other vars than the source run ("+differ+
				"), so a value it sent or asserted can differ for that reason alone: drop the -var to use the source run's", "\n")
		}
	case res.UnderIncluded:
		v.Outcome = sliceInconclusive
		names := droppedNames(res)
		v.Reason = fmt.Sprintf("the verdict matched, but the slice dropped %d write step(s): %s.\n"+
			"A match can come from state the slice never built, so it is not evidence that the slice reproduces\n"+
			"the failure. Keep the writes and verify again against the same source run; drop ids from -keep\n"+
			"to test which of them the target needs.", len(names), strings.Join(names, ", "))
		v.suggestKeep(res, rec, a, names)
	default:
		v.Outcome = sliceReproduced
		v.Reproduced = true
		if a.otherTarget != "" {
			v.Reason = a.otherTarget + ": the slice gave step " + res.Target + " the verdict it had there"
		}
	}
	return v, v.err()
}

func varsDifferBetween(source, replay *runner.Record, fresh map[string]bool) string {
	out := []string{}
	names := make([]string, 0, len(replay.Vars))
	for k := range replay.Vars {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		was, ok := source.Vars[k]
		if !ok || fresh[k] || fmt.Sprint(was) == pathmask.MaskRedacted {
			continue
		}
		if now := replay.Vars[k]; fmt.Sprint(now) != fmt.Sprint(was) {
			out = append(out, fmt.Sprintf("%s=%v, source %v", k, now, was))
		}
	}
	return strings.Join(out, "; ")
}

func (v *sliceVerdict) suggestKeep(res *chain.SliceResult, rec *runner.Record, a sliceVerifyArgs, names []string) {
	usable, blocked := failedInSource(rec, names)
	asked, askedBlocked := failedInSource(rec, a.keep)
	v.NotKeepable = append(append([]string{}, askedBlocked...), blocked...)
	if len(blocked) > 0 {
		v.Reason += fmt.Sprintf("\nLeft out of next: %s did not pass in source run %s. A slice that keeps it stops there\n"+
			"(-verify has no -keep-going), so step %s is never sent and the verdict can only be DID NOT RUN.",
			strings.Join(blocked, ", "), rec.RunID, res.Target)
	}
	if len(askedBlocked) > 0 {
		v.Reason += fmt.Sprintf("\nLeft out of next although you passed it with -keep: %s did not pass in source run %s.\n"+
			"With the dropped writes kept it can fail as it did there and stop the slice before step %s is sent,\n"+
			"so the verdict could only be DID NOT RUN.",
			strings.Join(askedBlocked, ", "), rec.RunID, res.Target)
	}
	a.keep = asked
	if len(usable) == 0 {
		v.Reason += "\nNo -keep command can reproduce this target: every dropped write it would need failed in the source run.\n" +
			"Fix those steps, or write a chain that reaches the target without them, run it, and verify a slice of that."
		return
	}
	v.Next = keepWritesCommand(res, rec.RunID, a, usable, len(blocked) == 0 && keepsEveryWriteCleanly(res, rec, a))
}

func verdictOf(sr *runner.StepRecord) chain.Verdict {
	var response any
	if len(sr.Response) > 0 {
		_ = json.Unmarshal(sr.Response, &response)
	}
	path := chain.EnvelopePath()
	code := ""
	if v, ok := chain.Get(response, path); ok {
		code = fmt.Sprintf("%v", v)
	}
	parent := ""
	if i := strings.LastIndex(path, "."); i >= 0 {
		parent = path[:i+1]
	}
	refusal := map[string]string{}
	for field, paths := range map[string][]string{
		"message":  {parent + "message"},
		"reason":   {parent + "details.0.reason", parent + "reason"},
		"app_code": {parent + "details.0.app_code", parent + "app_code"},
	} {
		for _, p := range paths {
			if v, ok := chain.Get(response, p); ok && v != nil && fmt.Sprint(v) != "" {
				refusal[field] = fmt.Sprint(v)
				break
			}
		}
	}
	if len(refusal) == 0 {
		refusal = nil
	}
	transport := ""
	if sr.Transport != nil {
		transport = sr.Transport.Code + ": " + sr.Transport.Message
		if sr.HTTPStatus != 0 {
			transport = fmt.Sprintf("HTTP %d %s", sr.HTTPStatus, transport)
		}
	}
	return chain.Verdict{Step: sr.ID, Status: sr.Status, ErrorCode: code, Refusal: refusal, Transport: transport, Expect: sr.Expect}
}

func sameUpToIDs(path string, a, b any) bool {
	if diff.LooksVolatile(path, a, b) {
		return true
	}
	x, ok1 := a.(string)
	y, ok2 := b.(string)
	if !ok1 || !ok2 {
		return false
	}
	wx, wy := strings.Fields(x), strings.Fields(y)
	if len(wx) != len(wy) || len(wx) < 2 {
		return false
	}
	for i := range wx {
		if wx[i] != wy[i] && !(idToken(wx[i]) && idToken(wy[i]) && diff.LooksVolatile("id", wx[i], wy[i])) {
			return false
		}
	}
	return true
}

func idToken(s string) bool {
	return strings.ContainsAny(s, "0123456789") && strings.IndexFunc(s, func(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }) >= 0
}

func freshSet(res *chain.SliceResult) map[string]bool {
	out := map[string]bool{}
	for _, name := range res.FreshVars {
		out[name] = true
	}
	if res.FreshVars == nil && res.Chain != nil {
		for _, name := range chain.FreshVars(res.Chain.Steps, nil, nil) {
			out[name] = true
		}
	}
	return out
}

func keepsEveryWriteCleanly(res *chain.SliceResult, rec *runner.Record, a sliceVerifyArgs) bool {
	if a.reslice == nil {
		return false
	}
	next := a.reslice(append(append([]string{}, a.keep...), chain.SliceKeepWrites))
	if next == nil || len(next.DroppedWrites) > 0 {
		return false
	}
	for _, k := range next.Kept {
		if k.ID == res.Target {
			continue
		}
		if sr, ok := rec.Step(k.ID); ok && (sr.Status == runner.StatusFailed || sr.Status == runner.StatusError) {
			return false
		}
	}
	return true
}

func keepWritesCommand(res *chain.SliceResult, runID string, a sliceVerifyArgs, dropped []string, allWrites bool) string {
	keep := append([]string{}, a.keep...)
	if allWrites {
		if !slices.Contains(keep, chain.SliceKeepWrites) {
			keep = append(keep, chain.SliceKeepWrites)
		}
	} else {
		keep = append(keep, dropped...)
	}
	parts := []string{"shrt chain slice", res.Source, "-step", res.Target}
	if res.Mode != chain.SliceModeClosure {
		parts = append(parts, "-mode", res.Mode)
	}
	parts = append(parts, "-run", runID, "-keep", strings.Join(keep, ","))
	interpolated := freshSet(res)
	if a.reslice != nil {
		if next := a.reslice(keep); next != nil {
			for name := range freshSet(next) {
				interpolated[name] = true
			}
		}
	}
	supplied := map[string]bool{}
	keys := make([]string, 0, len(a.vars)+len(interpolated))
	for k := range a.vars {
		supplied[k] = true
		keys = append(keys, k)
	}
	for k := range interpolated {
		if !supplied[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := fmt.Sprint(a.vars[k])
		if interpolated[k] {
			v = "<fresh>"
		}
		parts = append(parts, "-var", k+"="+v)
	}
	parts = append(parts, "-verify", "-write")
	if a.name != "" {
		parts = append(parts, a.name)
	}
	return strings.Join(parts, " ")
}

func droppedNames(res *chain.SliceResult) []string {
	names := make([]string, 0, len(res.DroppedWrites))
	for _, d := range res.DroppedWrites {
		names = append(names, d.ID)
	}
	return names
}

func writeSliceFile(path string, c *chain.Chain) error {
	raw, err := c.Marshal()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func mayOverwriteSlice(path string, res *chain.SliceResult, force, verify bool) error {
	if _, err := os.Stat(path); err != nil || force {
		return nil
	}
	existing, err := chain.LoadFile(path)
	if err != nil || existing.Name != res.Chain.Name ||
		!strings.HasPrefix(existing.Description, chain.SliceDescriptionPrefix(res.Source, res.Target)) {
		return fmt.Errorf("%s already exists and is not a slice of %s for step %s, pass -force to overwrite it or name another file: -write <name>",
			path, res.Source, res.Target)
	}
	if verify || !chain.HasVerifiedVerdict(existing.Description) {
		return nil
	}
	same := *res.Chain
	same.Description = existing.Description
	raw, rawErr := os.ReadFile(path)
	want, wantErr := same.Marshal()
	if rawErr == nil && wantErr == nil && string(raw) == string(want) {
		res.Chain.Description = existing.Description
		return nil
	}
	return fmt.Errorf("%s holds a slice VERIFIED by 'shrt chain slice -verify', and this slice differs from it (its steps, vars or pinned values), so writing it would drop that verdict.\n"+
		"Add -verify to verify the new slice in its place, pass -force to overwrite it unverified, or name another file: -write <name>", path)
}

func recordVars(rec *runner.Record) map[string]any {
	out := map[string]any{}
	for k, v := range rec.Vars {
		if text, ok := v.(string); ok && text == pathmask.MaskRedacted {
			continue
		}
		out[k] = v
	}
	return out
}

func refusedIn(rec *runner.Record) func(string) (string, bool) {
	return func(id string) (string, bool) {
		sr, ok := rec.Step(id)
		if !ok {
			return "", false
		}
		switch {
		case sr.Status == runner.StatusSkipped:
			return "not sent", true
		case sr.Transport != nil:
			return "refused: transport " + sr.Transport.Code, true
		}
		path := chain.EnvelopePath()
		if path == "" || len(sr.Response) == 0 {
			return "", false
		}
		var response any
		if err := json.Unmarshal(sr.Response, &response); err != nil {
			return "", false
		}
		v, found := chain.Get(response, path)
		if !found {
			return "", false
		}
		if code := fmt.Sprint(v); code != "" && code != chain.EnvelopeOK() {
			return fmt.Sprintf("refused: %s = %s", path, code), true
		}
		return "", false
	}
}

func performedIn(rec *runner.Record) func(string) bool {
	refused := refusedIn(rec)
	return func(id string) bool {
		sr, ok := rec.Step(id)
		if !ok || (sr.Status != runner.StatusPassed && sr.Status != runner.StatusFailed) {
			return false
		}
		_, wroteNothing := refused(id)
		return !wroteNothing
	}
}

func missingVarFlags(res *chain.SliceResult, rec *runner.Record) []string {
	interpolated := freshSet(res)
	out := make([]string, 0, len(res.MissingVars))
	for _, name := range res.MissingVars {
		value := "<value>"
		if interpolated[name] {
			value = "<fresh>"
		} else if rec != nil {
			if v, ok := rec.Vars[name]; ok {
				value = fmt.Sprint(v)
			}
		}
		out = append(out, "-var "+name+"="+value)
	}
	return out
}

func missingVarsError(res *chain.SliceResult, rec *runner.Record) error {
	flags := missingVarFlags(res, rec)
	msg := fmt.Sprintf("the slice reads var(s) %s, which %s does not declare and no -var supplied, so -verify would stop at an unresolved ${vars...}",
		strings.Join(res.MissingVars, ", "), res.Source)
	if res.Mode == chain.SliceModeClosure {
		msg += fmt.Sprintf(".\nClosure mode re-creates what the source run created, so a var interpolated into a name needs a value\n"+
			"the backend has not seen: reusing run %s's value collides with what that run made (a duplicate key refusal)", res.Run)
	}
	return fmt.Errorf("%s.\nPass: %s", msg, strings.Join(flags, " "))
}

func runRequired(mode string, verify bool) error {
	switch {
	case mode == chain.SliceModePin && verify:
		return fmt.Errorf("-mode pin and -verify need -run <run-id|latest>: pin mode pins values from that run, and -verify compares the target's verdict against it")
	case mode == chain.SliceModePin:
		return fmt.Errorf("-mode pin needs -run <run-id|latest>: it pins the values that run's steps produced, so without one there is nothing to pin")
	case verify:
		return fmt.Errorf("-verify needs -run <run-id|latest>: it compares the target's verdict against that run's, so without one there is nothing to compare against")
	}
	return nil
}

func reachedStep(rec *runner.Record, step string) (bool, string) {
	sr, ok := rec.Step(step)
	if !ok {
		return false, "not in run, " + rec.Status
	}
	if sr.Status == runner.StatusPassed || sr.Status == runner.StatusFailed {
		return true, sr.Status
	}
	return false, sr.Status
}

func newestRunReaching(e *env, chainName, step, skip string) (*runner.Record, error) {
	ids, err := e.store.ListRuns(chainName)
	if err != nil {
		return nil, err
	}
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] == skip {
			continue
		}
		rec, err := e.store.LoadRun(chainName, ids[i])
		if err != nil {
			continue
		}
		if ok, _ := reachedStep(rec, step); ok {
			return rec, nil
		}
	}
	return nil, nil
}

func loadRunReaching(e *env, chainName, runID, step string) (*runner.Record, error) {
	if runID == "latest" {
		latest, err := e.store.LatestRun(chainName)
		if err != nil {
			return nil, err
		}
		if ok, _ := reachedStep(latest, step); ok {
			return latest, nil
		}
		_, why := reachedStep(latest, step)
		rec, err := newestRunReaching(e, chainName, step, latest.RunID)
		if err != nil {
			return nil, err
		}
		if rec == nil {
			return nil, fmt.Errorf("no recorded run of %s reached step %q (the newest, %s, stopped before it: %s), so there is no value to pin and no verdict to compare.\n"+
				"Run the chain until it reaches the step: shrt run %s", chainName, step, latest.RunID, why, chainName)
		}
		fmt.Fprintf(os.Stderr, "note: -run latest is run %s, the newest run of %s that reached step %s; the newest run, %s, did not (%s)\n",
			rec.RunID, chainName, step, latest.RunID, why)
		return rec, nil
	}
	rec, err := e.store.LoadRun(chainName, runID)
	if err != nil {
		return nil, err
	}
	ok, why := reachedStep(rec, step)
	if ok {
		return rec, nil
	}
	msg := fmt.Sprintf("run %s did not reach step %q (%s), so it has no value to pin and no verdict to compare", rec.RunID, step, why)
	other, err := newestRunReaching(e, chainName, step, rec.RunID)
	if err != nil {
		return nil, err
	}
	if other == nil {
		return nil, fmt.Errorf("%s, and no recorded run of %s reached it.\nRun the chain until it reaches the step: shrt run %s", msg, chainName, chainName)
	}
	return nil, fmt.Errorf("%s.\nRun %s did: pass -run %s (or -run latest, which picks the newest run that reached the step)", msg, other.RunID, other.RunID)
}

func freshVarsError(res *chain.SliceResult, source *chain.Chain, rec *runner.Record, supplied varFlags) error {
	reused := []string{}
	from := []string{}
	for _, name := range res.FreshVars {
		if _, given := supplied[name]; given {
			continue
		}
		reused = append(reused, name)
		switch v, ok := res.Chain.Vars[name]; {
		case rec != nil && rec.Vars[name] != nil:
			from = append(from, fmt.Sprintf("%s=%v, the value run %s used", name, rec.Vars[name], rec.RunID))
		case !ok:
			from = append(from, name+" unset")
		case fmt.Sprint(source.Vars[name]) == fmt.Sprint(v):
			from = append(from, fmt.Sprintf("%s=%v, the default %s declares", name, v, source.Name))
		default:
			from = append(from, fmt.Sprintf("%s=%v", name, v))
		}
	}
	if len(reused) == 0 {
		return nil
	}
	flags := make([]string, 0, len(reused))
	for _, name := range reused {
		flags = append(flags, "-var "+name+"=<fresh>")
	}
	return fmt.Errorf("the slice keeps write step(s) that interpolate var(s) %s into what they create, so -verify would re-send them with %s.\n"+
		"That value is not fresh: the source run, or an earlier verify, already created with it, and sending it again collides (a duplicate key refusal).\n"+
		"Pass: %s, replacing each <fresh> with a value this backend has not seen",
		strings.Join(reused, ", "), strings.Join(from, "; "), strings.Join(flags, " "))
}

func recordVerdictIn(path string, record func(string) string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc := &yaml.Node{}
	if err := yaml.Unmarshal(raw, doc); err != nil {
		return err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s is not a chain mapping, so the verdict cannot be recorded in it", path)
	}
	top := doc.Content[0]
	current := ""
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "description" {
			current = top.Content[i+1].Value
		}
	}
	value := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: record(current), Style: yaml.LiteralStyle}
	setKey(top, "description", value)
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func failedInSource(rec *runner.Record, ids []string) ([]string, []string) {
	usable, blocked := []string{}, []string{}
	for _, id := range ids {
		sr, ok := rec.Step(id)
		if ok && (sr.Status == runner.StatusFailed || sr.Status == runner.StatusError) {
			blocked = append(blocked, id+" ("+sr.Status+")")
			continue
		}
		usable = append(usable, id)
	}
	return usable, blocked
}
