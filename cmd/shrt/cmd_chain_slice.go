package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

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
	force := fs.Bool("force", false, "with -write, overwrite an existing chain file that is not this command's slice of the same step")
	vars := varFlags{}
	fs.Var(vars, "var", "set a var, repeatable: -var key=value; overrides a chain var when running -verify, and a var the chain does not declare is written into the slice")
	write := &optionalString{}
	fs.Var(write, "write", "write the slice to .shrt/chains/<name>.yaml; `[name]` is optional (-write, or -write <name>) and defaults to <chain>-slice-<step-id>")
	keep := &stepList{}
	fs.Var(keep, "keep", "also keep these earlier steps and what they need, comma-separated or repeated: -keep `id[,id]`")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) == 0 || len(rest) > 2 {
		return fmt.Errorf("usage: shrt chain slice <chain> -step <step-id> [-mode closure|pin] [-run <id>] [-keep <id,...>] [-var k=v] [-write [<name>] [-force]] [-verify] [-json]")
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

	opts := chain.SliceOptions{Mode: *mode, Name: name, RPCOf: rpcOf(e), Keep: *keep, Vars: vars}
	lib, _, libErr := e.library()
	if libErr == nil && lib != nil {
		opts.Prereqs = contract.PrereqsFor(lib)
	}
	var rec *runner.Record
	if (*mode == chain.SliceModePin || *verify) && *runID == "" {
		return fmt.Errorf("-run <run-id|latest> is required by -mode %s and by -verify: without a run record there is nothing to pin or to compare against", *mode)
	}
	if *runID != "" {
		rec, err = e.store.LoadRun(c.Name, *runID)
		if err != nil {
			return err
		}
		opts.RunID = rec.RunID
		opts.Value = recordValues(rec)
		opts.RunVars = recordVars(rec)
		opts.Refused = refusedIn(rec)
	}

	res, err := chain.Slice(c, *step, opts)
	if err != nil {
		return err
	}
	if *verify && len(res.MissingVars) > 0 {
		return missingVarsError(res, rec)
	}

	written := ""
	if write.set {
		path := filepath.Join(e.chainsDir(), res.Chain.Name+".yaml")
		if err := mayOverwriteSlice(path, res, *force); err != nil {
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
		verdict, verifyErr = runSliceVerify(ctx, e, res, rec, sliceVerifyArgs{
			vars: vars, quiet: *asJSON, persist: write.set, name: name, keep: *keep,
		})
		if verdict == nil {
			return verifyErr
		}
		if verdict.Outcome == sliceReproduced && written != "" {
			res.MarkReproduced(verdict.SourceRun, verdict.SliceRun, time.Now())
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
	if len(res.FilledVars) > 0 {
		fmt.Println("\nvars the chain does not declare, written into the slice:")
		for _, f := range res.FilledVars {
			from := "from -var"
			if f.From == chain.VarFromRun {
				from = "the value run " + res.Run + " used"
			}
			fmt.Printf("  %s = %v  (%s)\n", f.Var, f.Value, from)
		}
	}
	if len(res.MissingVars) > 0 {
		fmt.Println("\nvars the kept steps read that the chain does not declare and nothing supplied; the slice")
		fmt.Printf("cannot run without them, pass: %s\n", strings.Join(missingVarFlags(res, nil), " "))
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
	Next         string        `json:"next,omitempty"`
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
		Vars: a.vars, Volatile: e.cfg.Volatile, Redact: e.cfg.Redact,
	}, a.quiet)
	if err != nil {
		return didNotRun("could not run the slice: " + err.Error())
	}
	v.SliceRun = replayRec.RunID
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
		if res.UnderIncluded {
			names := droppedNames(res)
			v.Reason = fmt.Sprintf("the slice dropped %d write step(s): %s.\n"+
				"The difference can come from state those writes would have built. Keep them and verify again\n"+
				"against the same source run; if the verdict then matches, drop ids from -keep to find the one\n"+
				"the target needs.", len(names), strings.Join(names, ", "))
			v.Next = keepWritesCommand(res, rec.RunID, a, names)
		}
	case res.UnderIncluded:
		v.Outcome = sliceInconclusive
		names := droppedNames(res)
		v.Reason = fmt.Sprintf("the verdict matched, but the slice dropped %d write step(s): %s.\n"+
			"A match can come from state the slice never built, so it is not evidence that the slice reproduces\n"+
			"the failure. Keep the writes and verify again against the same source run; drop ids from -keep\n"+
			"to test which of them the target needs.", len(names), strings.Join(names, ", "))
		v.Next = keepWritesCommand(res, rec.RunID, a, names)
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

var interpolatedRef = regexp.MustCompile(`\$\{vars\.([A-Za-z0-9_]+)\}`)

func interpolatedVars(c *chain.Chain) map[string]bool {
	out := map[string]bool{}
	if c == nil {
		return out
	}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			for _, m := range interpolatedRef.FindAllStringSubmatchIndex(t, -1) {
				if m[0] != 0 || m[1] != len(t) {
					out[t[m[2]:m[3]]] = true
				}
			}
		case map[string]any:
			for _, item := range t {
				walk(item)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	for _, st := range c.Steps {
		walk(st.Body)
	}
	return out
}

func keepWritesCommand(res *chain.SliceResult, runID string, a sliceVerifyArgs, dropped []string) string {
	keep := append([]string{}, a.keep...)
	keep = append(keep, dropped...)
	parts := []string{"shrt chain slice", res.Source, "-step", res.Target}
	if res.Mode != chain.SliceModeClosure {
		parts = append(parts, "-mode", res.Mode)
	}
	parts = append(parts, "-run", runID, "-keep", strings.Join(keep, ","))
	interpolated := interpolatedVars(res.Chain)
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

func mayOverwriteSlice(path string, res *chain.SliceResult, force bool) error {
	if _, err := os.Stat(path); err != nil || force {
		return nil
	}
	existing, err := chain.LoadFile(path)
	if err == nil && existing.Name == res.Chain.Name &&
		strings.HasPrefix(existing.Description, chain.SliceDescriptionPrefix(res.Source, res.Target)) {
		return nil
	}
	return fmt.Errorf("%s already exists and is not a slice of %s for step %s, pass -force to overwrite it or name another file: -write <name>",
		path, res.Source, res.Target)
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

func missingVarFlags(res *chain.SliceResult, rec *runner.Record) []string {
	interpolated := interpolatedVars(res.Chain)
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
