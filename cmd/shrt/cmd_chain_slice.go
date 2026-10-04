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

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"gopkg.in/yaml.v3"
)

const sliceUsage = "usage: shrt chain slice <chain> -step <id> [flags]\n" +
	"       shrt chain slice <chain> -without <id,...|failed> [-run <id>] [-write [<name>]] [-verify]"

const sliceExitCodes = "\nexit codes:\n" +
	"  0  printed or written; -verify: reproduced\n" +
	"  1  refused; -verify: NOT REPRODUCED, intermittent, or STILL FAILS without\n" +
	"  3  -run latest did not evaluate the step; -verify: DID NOT RUN, INCONCLUSIVE, or FAILS DIFFERENTLY without\n"

const sliceRepeat = 3

func chainSlice(ctx context.Context, args []string) error {
	return sliceChain(ctx, args, nil)
}

func sliceChain(ctx context.Context, args []string, keptRed *sliceKeptRed) error {
	fs := flag.NewFlagSet("chain slice", flag.ContinueOnError)
	step := fs.String("step", "", "step `id` the slice must reproduce")
	runID := fs.String("run", "", "run record `id` or latest; -verify defaults to latest")
	verify := fs.Bool("verify", false, "run the slice and compare the step's verdict with the run record")
	asJSON := fs.Bool("json", false, "print JSON")
	verbose := fs.Bool("v", false, "also print each kept step and why, the vars written, and the dropped writes")
	vars := varFlags{}
	fs.Var(vars, "var", "set a var as `key=value`, repeatable")
	write := &optionalString{}
	fs.Var(write, "write", "write .shrt/chains/`[name]`.yaml (default <chain>-slice-<step>), or a path with a slash")
	without := &stepList{}
	fs.Var(without, "without", "leave out these steps and those reading them: `id[,id]|failed`")
	keep := &stepList{}
	fs.Var(keep, "keep", "also keep these earlier `id[,id]` steps; writes keeps every earlier write")
	setUsage(fs, sliceUsage, sliceExitCodes)
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if keptRed == nil {
		keptRed = &sliceKeptRed{}
	}
	if len(rest) == 0 || len(rest) > 2 {
		return errors.New(sliceUsage)
	}
	name := write.value
	for _, v := range append([]string{name}, rest[1:]...) {
		if strings.HasPrefix(v, "=") {
			return fmt.Errorf("unexpected argument %q: did you mean -write%s (no space before the equals sign)?", v, v)
		}
	}
	if len(rest) == 2 {
		if !write.set || name != "" {
			return fmt.Errorf("unexpected argument %q\n\n%s", rest[1], sliceUsage)
		}
		name = rest[1]
	}
	if len(*without) > 0 {
		if *step != "" || len(*keep) > 0 {
			return fmt.Errorf("-without writes the chain minus some steps, not a slice: it takes no -step or -keep")
		}
		var wv *withoutVerify
		if *verify {
			wv = &withoutVerify{vars: vars}
		}
		return sliceWithout(ctx, rest[0], *without, *runID, write, name, *asJSON, wv)
	}
	if *step == "" {
		return fmt.Errorf("-step is required: name the step the slice must reproduce")
	}
	keptRed.steps = slices.DeleteFunc(append([]string{}, keptRed.steps...), func(id string) bool { return id == *step })
	if (keptRed.on || *verify) && *runID == "" {
		*runID = "latest"
	}
	writePath, writeArg := "", name
	bare := bareSliceFile(name)
	if bare {
		name = strings.TrimSuffix(name, ".yaml")
	} else if isSlicePath(name) {
		writePath, name, err = slicePathAndName(name)
		if err != nil {
			return err
		}
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	c, err := chain.Resolve(e.chainsDir(), rest[0])
	if err != nil {
		return err
	}
	ref := sliceChainRef(rest[0], c)
	if bare {
		writePath = filepath.Join(sliceDir(e, c), name+".yaml")
	}

	opts := chain.SliceOptions{Name: name, RPCOf: rpcOf(e), Keep: append(append([]string{}, *keep...), keptRed.steps...), Pinned: keptRed.steps, Vars: vars, IsLogin: isLoginStep(e)}
	lib, err := e.library()
	if err != nil {
		return err
	}
	opts.Prereqs = contract.PrereqsFor(lib)
	opts.KeyField = contract.KeyFieldFor(lib)
	var rec *runner.Record
	if *runID != "" {
		_, known := c.Step(*step)
		switch {
		case known && (*verify || keptRed.on):
			rec, err = loadRunReaching(e, c.Name, ref, *runID, *step)
		case *runID == "latest":
			rec, err = latestRun(e, c.Name, *step)
		default:
			rec, err = e.store.LoadRun(c.Name, *runID)
		}
		if err != nil {
			return err
		}
		opts.RunID = rec.RunID
		opts.RunVars = recordVars(rec)
		opts.RunVarsAsDefaults = *verify
		opts.Refused = refusedIn(rec)
		if !keptRed.on {
			opts.Relax = relaxIn(rec)
		}
		opts.StateIrrelevant = stateIrrelevantIn(e, lib, c, rec)
		opts.AssertsWrite = func(w, r string) bool { return assertsWritten(rec, w, r, entityFactsOf(rec, r).mentions) }
	}

	res, err := chain.Slice(c, *step, opts)
	if err != nil {
		return err
	}
	if keptRed.on && rec != nil {
		if opts.Checkpoints = checkpointReads(c, res, rec, append([]string{*step}, keptRed.steps...)); len(opts.Checkpoints) > 0 {
			if res, err = chain.Slice(c, *step, opts); err != nil {
				return err
			}
		}
	}
	res.SourceRef = ref
	redPins := []chain.Pin{}
	inherited := len(res.Chain.KeptRed)
	if keptRed.on {
		if redPins, err = failurePins(res.Chain, rec, *step); err != nil {
			return err
		}
		stepPins, err := keptStepPins(res, rec, keptRed.steps)
		if err != nil {
			return err
		}
		res.Chain.KeptRed = append(res.Chain.KeptRed, stepPins...)
		if !*verify {
			res.Chain.KeptRed = append(res.Chain.KeptRed, redPins...)
		}
	}
	if *verify && len(res.MissingVars) > 0 {
		return missingVarsError(res, rec)
	}
	if *verify {
		if err := freshVarsError(res, c, rec, vars); err != nil {
			return err
		}
	}

	whole := write.set && name == "" && len(res.Kept) == res.Total && c.SourcePath != "" && !keptRed.on
	if whole {
		res.Chain.Name = c.Name
	}
	written, created := "", false
	if whole {
		if !*asJSON {
			fmt.Printf("the slice keeps all %d steps of %s, so it is %s itself: nothing written\n", res.Total, c.Name, c.Name)
		}
	} else if write.set {
		path := filepath.Join(sliceDir(e, c), res.Chain.Name+".yaml")
		if writePath != "" {
			path = writePath
		}
		source := writePath != "" && sameFile(path, c.SourcePath)
		if source {
			res.Chain.Name = c.Name
		}
		if err := mayOverwriteSlice(path, res, source, *verify); err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			created = true
		}
		if err := writeSliceFile(path, res.Chain); err != nil {
			return err
		}
		written = path
		res.Chain.SourcePath = rel(e.cfg.Root, path)
	}

	var verdict *sliceVerdict
	var verifyErr error
	if *verify {
		if !*asJSON {
			printSliceHeader(res)
		}
		verdict, verifyErr = runSliceVerifyRepeated(ctx, e, res, rec, sliceVerifyArgs{
			vars: vars, quiet: *asJSON, verbose: *verbose, persist: write.set, name: writeArg, keep: *keep,
			otherTarget: sourceTargetDiffers(e, rec),
			reslice: func(keep []string) *chain.SliceResult {
				o := opts
				o.Keep = append(append([]string{}, keep...), keptRed.steps...)
				next, err := chain.Slice(c, *step, o)
				if err != nil {
					return nil
				}
				return next
			},
		}, sliceRepeat)
		if verdict == nil {
			return verifyErr
		}
		keptRed.verdict = verdict
		if err := recordSliceVerdict(res, c, verdict, whole); err != nil {
			return err
		}
		if keptRed.on && verdict.Outcome == sliceReproduced && !whole {
			if verdict.replay != nil {
				res.Chain.KeptRed = res.Chain.KeptRed[:inherited]
				res.Chain.KeptRed = append(res.Chain.KeptRed, replayPins(res, verdict.replay)...)
			} else {
				res.Chain.KeptRed = append(res.Chain.KeptRed, redPins...)
			}
		}
		if keptRed.on && verdict.Outcome != sliceReproduced && written != "" && created {
			if err := os.Remove(written); err != nil {
				return err
			}
			written = ""
		}
		if verdict.Outcome != sliceDidNotRun && written != "" {
			if err := writeSliceFile(written, res.Chain); err != nil {
				return err
			}
			verdict.Recorded = written
		}
	}

	if *asJSON {
		if err := emitJSON(struct {
			*chain.SliceResult
			Written string        `json:"written,omitempty"`
			Verify  *sliceVerdict `json:"verify,omitempty"`
		}{SliceResult: res, Written: written, Verify: verdict}); err != nil {
			return err
		}
		return verifyErr
	}

	if !*verify {
		printSliceHeader(res)
	}
	printSlice(res, written, verdict, *verbose)
	if written != "" && !sameFile(written, c.SourcePath) && len(res.Chain.KeptRed) == 0 {
		fmt.Print(sweepNote(e, written))
	}
	if !write.set && !*verify {
		raw, err := res.Chain.Marshal()
		if err != nil {
			return err
		}
		fmt.Printf("\n%s\n", string(raw))
	}
	return verifyErr
}

func recordSliceVerdict(res *chain.SliceResult, c *chain.Chain, verdict *sliceVerdict, whole bool) error {
	res.Build = verdict.Build
	now := time.Now()
	first := ""
	if len(verdict.Differences) > 0 {
		first = verdict.Differences[0]
	}
	why, _, _ := strings.Cut(verdict.Reason, "\n")
	why = strings.TrimSuffix(why, ".")
	rerun := whole && chain.IsSliceDescription(c.Description)
	switch {
	case verdict.Outcome == sliceReproduced && whole:
		res.Verified = res.OwnRunVerdict(c.Name, verdict.SourceRun, verdict.runsLabel(), now)
		record := chain.RecordVerified
		if rerun {
			record = chain.RecordRerun
		}
		if err := recordVerdictIn(c.SourcePath, func(d string) string { return record(d, res.Verified) }); err != nil {
			return err
		}
		verdict.Recorded = c.SourcePath
	case rerun && verdict.Outcome != sliceDidNotRun:
		outcome, detail := "not reproduced", first
		switch verdict.Outcome {
		case sliceInconclusive:
			outcome, detail = "INCONCLUSIVE", why
		case sliceIntermittent:
			outcome, detail = fmt.Sprintf("INTERMITTENT, reproduced %d/%d", verdict.ReproducedRuns, verdict.counted()), ""
		}
		line := res.OwnRunOutcome(outcome, c.Name, verdict.SourceRun, verdict.runsLabel(), now, detail)
		if err := recordVerdictIn(c.SourcePath, func(d string) string { return chain.RecordRerun(d, line) }); err != nil {
			return err
		}
		verdict.Recorded = c.SourcePath
	case verdict.Outcome == sliceReproduced:
		res.MarkReproduced(verdict.SourceRun, verdict.runsLabel(), now)
	case whole:
	case verdict.Outcome == sliceNotReproduced:
		res.MarkNotReproduced(verdict.SourceRun, verdict.runsLabel(), now, first)
	case verdict.Outcome == sliceInconclusive:
		res.MarkInconclusive(verdict.SourceRun, verdict.runsLabel(), now, why)
	case verdict.Outcome == sliceIntermittent:
		res.MarkIntermittent(verdict.SourceRun, verdict.runsLabel(), verdict.ReproducedRuns, verdict.counted(), now)
	}
	return nil
}

const hypothesisLine = "unverified: a slice is a hypothesis until -verify reproduces the step's verdict"

func printSliceHeader(res *chain.SliceResult) {
	fmt.Printf("slice of %s for step %s", res.Source, res.Target)
	if res.Run != "" {
		fmt.Printf(", run %s", res.Run)
	}
	fmt.Println()
}

func printSlice(res *chain.SliceResult, written string, verdict *sliceVerdict, verbose bool) {
	if verbose {
		fmt.Println()
		printSliceDetail(res)
	}
	if len(res.FreshVars) > 0 {
		fmt.Printf("\nkept writes create with %s, so each run needs a value this backend has not seen: %s\n",
			strings.Join(res.FreshVars, ", "), freshFlags(res.FreshVars))
	}
	if len(res.MissingVars) > 0 {
		fmt.Printf("\nundeclared vars the kept steps read, pass: %s\n", strings.Join(missingVarFlags(res, nil), " "))
	}
	if len(res.Unmet) > 0 {
		fmt.Println("\nunmet prerequisites, no earlier step calls them:")
		for _, u := range res.Unmet {
			fmt.Printf("  %s %s (declared for %s)\n", u.Edge, u.RPC, u.Step)
		}
	}
	if len(res.Relaxed) > 0 {
		fmt.Printf("\nrelaxed: expectations kept steps failed in run %s after an answer, dropped so the slice reaches the target:\n", res.Run)
		for _, r := range res.Relaxed {
			fmt.Printf("  %s\n", r.String())
		}
	}
	printSlicePins(res)
	fmt.Printf("\n%s\n", sliceCountLine(res, verdict))
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

func printSliceDetail(res *chain.SliceResult) {
	idW, callW := 0, 0
	for _, k := range res.Kept {
		if n := len(k.ID); n > idW {
			idW = n
		}
		if n := len(shortRPC(k.Call)); n > callW {
			callW = n
		}
	}
	for _, k := range res.Kept {
		fmt.Printf("  %4d  %-*s  %-*s  %s\n", k.Index, idW, k.ID, callW, shortRPC(k.Call), k.Reason)
	}
	if len(res.FilledVars) > 0 {
		fmt.Println("\nvars written into the slice:")
		for _, f := range res.FilledVars {
			from := "-var"
			if f.From == chain.VarFromRun {
				from = "run " + res.Run
			}
			fmt.Printf("  %s = %v  (from %s)\n", f.Var, f.Value, from)
		}
	}
	if len(res.DroppedWrites) > 0 {
		fmt.Println("\ndropped write steps:")
		for _, d := range res.DroppedWrites {
			fmt.Printf("  %4d  %s  %s\n", d.Index, d.ID, shortRPC(d.Call))
		}
	}
	for _, d := range res.RefusedWrites {
		fmt.Printf("  %4d  %s  %s  %s in run %s, not counted\n", d.Index, d.ID, shortRPC(d.Call), d.Reason, res.Run)
	}
}

func sliceCountLine(res *chain.SliceResult, verdict *sliceVerdict) string {
	line := fmt.Sprintf("kept %d of %d steps, dropped %d", len(res.Kept), res.Total, res.Total-len(res.Kept))
	named := verdict != nil && verdict.Outcome == sliceReproduced && len(verdict.OtherDropped) == len(res.DroppedWrites)
	if len(res.DroppedWrites) == 0 || named {
		return line
	}
	names := make([]string, 0, len(res.DroppedWrites))
	for _, d := range res.DroppedWrites {
		names = append(names, d.ID)
	}
	line += fmt.Sprintf(", writes among them %s", capList(names, 3))
	if res.UnderIncluded {
		line += ": WARNING possible under-inclusion, a kept step may depend on state they left"
	}
	return line
}

func printSlicePins(res *chain.SliceResult) {
	carried := len(res.CarriedPins)
	if carried == 0 && len(res.DroppedPins) == 0 {
		return
	}
	dropped := make([]string, 0, len(res.DroppedPins))
	for _, k := range res.DroppedPins {
		dropped = append(dropped, k.Step+" "+k.Path)
	}
	switch {
	case len(dropped) == 0:
		fmt.Printf("\nkept_red: carries all %d pin(s) of %s\n", carried, res.Source)
	default:
		fmt.Printf("\nkept_red: carries %d pin(s) of %s, drops those on steps it does not keep: %s\n", carried, res.Source, strings.Join(dropped, ", "))
	}
}

func sweepNote(e *env, written string) string {
	shown := shownPath(written)
	if filepath.Dir(written) != filepath.Clean(e.chainsDir()) {
		return fmt.Sprintf("note: no sweep reads %s; run it by path: shrt run %s\n", shown, shown)
	}
	return fmt.Sprintf("note: lint, hollow and the gate run %s; to keep it out: mkdir -p .shrt/scratch && mv %s .shrt/scratch/\n", shown, shown)
}

func sourceFileArg(c *chain.Chain) string {
	if c.SourcePath == "" {
		return c.Name + ".yaml"
	}
	return shownPath(c.SourcePath)
}

func shownPath(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	r, err := filepath.Rel(wd, path)
	if err != nil || strings.HasPrefix(r, "..") {
		return path
	}
	return filepath.ToSlash(r)
}

func isSlicePath(value string) bool {
	return strings.ContainsAny(value, "/\\") || strings.HasSuffix(value, ".yaml")
}

func bareSliceFile(value string) bool {
	return strings.HasSuffix(value, ".yaml") && !strings.ContainsAny(value, "/\\") && !strings.HasPrefix(value, ".")
}

func slicePathAndName(value string) (string, string, error) {
	path := value
	if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
		path += ".yaml"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	name := strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	if name == "" || strings.HasPrefix(name, ".") {
		return "", "", fmt.Errorf("-write %s: the file name must name the chain, as in -write .shrt/scratch/<name>.yaml", value)
	}
	return abs, name, nil
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

const (
	sliceReproduced    = "reproduced"
	sliceNotReproduced = "not_reproduced"
	sliceInconclusive  = "inconclusive"
	sliceDidNotRun     = "did_not_run"
)

type sliceVerdict struct {
	Step           string            `json:"step"`
	Outcome        string            `json:"outcome"`
	Reproduced     bool              `json:"reproduced"`
	Reason         string            `json:"reason,omitempty"`
	EnvelopePath   string            `json:"envelope_path"`
	SourceRun      string            `json:"source_run"`
	SliceRun       string            `json:"slice_run,omitempty"`
	Build          string            `json:"build,omitempty"`
	SliceRecord    string            `json:"slice_record,omitempty"`
	Status         string            `json:"slice_status,omitempty"`
	Source         chain.Verdict     `json:"source"`
	Replay         chain.Verdict     `json:"replay"`
	Differences    []string          `json:"differences,omitempty"`
	Next           string            `json:"next,omitempty"`
	Prove          string            `json:"prove,omitempty"`
	NotKeepable    []string          `json:"not_keepable,omitempty"`
	OtherDropped   []string          `json:"dropped_writes_other_entities,omitempty"`
	Repeat         int               `json:"repeat,omitempty"`
	ReproducedRuns int               `json:"reproduced_runs,omitempty"`
	MatchedRuns    int               `json:"verdict_matched_runs,omitempty"`
	Runs           []sliceRunOutcome `json:"runs,omitempty"`
	OtherTarget    string            `json:"source_target_differs,omitempty"`
	BlockedBy      []string          `json:"unevaluated_behind,omitempty"`
	Recorded       string            `json:"verdict_written_to,omitempty"`
	OutsideState   []string          `json:"outside_state,omitempty"`
	ByDistance     []string          `json:"compared_by_distance,omitempty"`
	Drifted        []string          `json:"drifted,omitempty"`
	SourceReplay   string            `json:"source_replay_of,omitempty"`
	replay         *runner.Record
	nextKeep       []string
	brokeWhy       string
	matched        bool
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
	verbose bool
	persist bool
	name    string
	keep    []string
	reslice func(keep []string) *chain.SliceResult

	otherTarget string
}

func sourceTargetDiffers(e *env, rec *runner.Record) string {
	if rec == nil || !e.otherTarget(rec.Target) {
		return ""
	}
	return fmt.Sprintf("the source run was recorded against %s, this target is %s", rec.Target, e.targetURL())
}

func (v *sliceVerdict) text() string {
	var b strings.Builder
	head := outcomeWord(v.Outcome)
	if v.Outcome == sliceIntermittent {
		head += ": reproduced"
	}
	head += v.countLabel()
	slice := v.SliceRun
	if slice == "" {
		slice = "none"
	}
	source := v.SourceRun
	if v.SourceReplay != "" {
		source += " (a shrt verify replay)"
	}
	runs := ""
	switch {
	case v.Repeat <= 1 || v.Outcome == sliceReproduced || v.Outcome == sliceIntermittent:
	case v.MatchedRuns > 0:
		runs = fmt.Sprintf(" the verdict matched in %d of %d slice runs,", v.MatchedRuns, v.counted())
	default:
		runs = fmt.Sprintf(" %d slice runs,", v.Repeat)
	}
	if v.Repeat > 1 {
		fmt.Fprintf(&b, "verify %s: step %s, source run %s,%s details from slice run %s", head, v.Step, source, runs, slice)
	} else {
		fmt.Fprintf(&b, "verify %s: step %s, source run %s, slice run %s", head, v.Step, source, slice)
	}
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
		for _, line := range slices.Concat(v.ByDistance, v.Drifted) {
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
		b.WriteString("  slice run not kept: add -write to keep the slice and its run record\n")
	}
	if v.Recorded != "" {
		fmt.Fprintf(&b, "  verdict recorded in the description of %s\n", v.Recorded)
	}
	if v.Prove != "" {
		fmt.Fprintf(&b, "  whether a dropped write caused it: %s\n", v.Prove)
	}
	if v.Next != "" {
		fmt.Fprintf(&b, "  next: %s\n", v.Next)
	}
	return b.String()
}

func failedExpectLines(source, replay chain.Verdict) []string {
	out := []string{}
	used := map[int]bool{}
	for i, e := range source.Expect {
		if e.Passed {
			continue
		}
		if _, held := blockedBy(e); held {
			if len(replay.Expect) == len(source.Expect) {
				used[i] = true
			}
			continue
		}
		got := "not evaluated"
		for i, r := range replay.Expect {
			if !used[i] && r.Path == e.Path && r.Rule == e.Rule {
				used[i] = true
				got = chain.GotText(r.Rule, quoted(r.Got))
				if r.Passed {
					got += " (held)"
				}
				break
			}
		}
		out = append(out, fmt.Sprintf("failed: %s %s source %s, slice %s", e.Path, chain.WantText(e.Rule, quoted(e.Want)), chain.GotText(e.Rule, quoted(e.Got)), got))
	}
	for i, r := range replay.Expect {
		if !r.Passed && !used[i] {
			out = append(out, fmt.Sprintf("failed in the slice only: %s %s", r.Path, chain.WantGot(r.Rule, quoted(r.Want), quoted(r.Got))))
		}
	}
	return out
}

func quoted(v any) string {
	if v == nil {
		return "(absent)"
	}
	if text, ok := v.(string); ok {
		return chain.EdgeQuoted(text)
	}
	return exportJSON(v)
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
	case sliceIntermittent:
		return exitWith(1, "step %s: intermittent, the slice reproduced it in %d of %d runs", v.Step, v.ReproducedRuns, v.counted())
	case sliceNotReproduced:
		return exitWith(1, "step %s was NOT reproduced by the slice", v.Step)
	case sliceDidNotRun:
		return exitWith(3, "the slice DID NOT RUN step %s, so nothing was verified", v.Step)
	case sliceInconclusive:
		if v.OtherTarget != "" {
			return exitWith(3, "step %s: INCONCLUSIVE, %s", v.Step, v.OtherTarget)
		}
		if len(v.BlockedBy) > 0 {
			return exitWith(3, "step %s: INCONCLUSIVE, its failing expectations were not evaluated in the source run, behind %s", v.Step, strings.Join(v.BlockedBy, ", "))
		}
		why, _, _ := strings.Cut(v.Reason, "\n")
		return exitWith(3, "step %s: INCONCLUSIVE, %s", v.Step, why)
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
		EnvelopePath: verdictPath(source),
		SourceRun:    rec.RunID,
		SourceReplay: rec.ReplayOf,
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
	v.replay = replayRec
	if broke := keptStepsBroken(replayRec, rec, res.Target); len(broke) > 0 {
		v.brokeWhy = keptFailureWhy(replayRec, broke)
	}
	v.Build = replayRec.Build
	v.Status = replayRec.Status
	if a.persist {
		if path, saveErr := e.store.SaveRun(replayRec); saveErr == nil {
			v.SliceRecord = path
		}
	}
	replay, ok := replayRec.Step(res.Target)
	if !ok {
		v.Outcome = sliceDidNotRun
		v.Reason = fmt.Sprintf("the slice run stopped before step %q (%s): %s", res.Target, replayRec.Status, replayRec.Failure)
		if note := sliceCollisionNote(e, res, replayRec); note != "" {
			v.Reason += "\n" + note
			return v, v.err()
		}
		if at := stoppedWhereSourcePassed(replayRec, rec); at != "" {
			if related, other := relatedDroppedWrites(res, rec); len(related) > 0 {
				v.Reason += fmt.Sprintf("\n%s passed in source run %s and fails here; dropped writes on its entities: %s", at, rec.RunID, capList(related, 5))
				v.OtherDropped = other
				v.suggestKeep(res, rec, a, related)
			}
		}
		return v, v.err()
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
	target, _ := res.Chain.Step(res.Target)
	same := sameUpToFixtures(rec.Vars, replayRec.Vars)
	v.Differences = compareVerdictsAt(target, v.Source, v.Replay, same, "slice")
	v.ByDistance = clockDistanceLines(res, v.Source, v.Replay, same)
	blocked := blockedReads(v.Source, v.Replay)
	upstreamOnly := false
	if evaluatedBlocked(blocked) {
		v.Differences = compareVerdictsAt(target, v.Source, withoutBlocked(v.Source, v.Replay, blocked), same, "slice")
		upstreamOnly = len(v.Differences) == 0
	}
	driftUnseen := false
	if len(v.Differences) == 0 {
		passed := v.Source.Status == runner.StatusPassed
		missing, drifted, compared := sliceDrift(e, res, rec, replayRec, passed)
		v.Differences, v.Drifted, driftUnseen = missing, drifted, passed && !compared
	}
	v.matched = len(v.Differences) == 0 && !upstreamOnly
	related, other := relatedDroppedWrites(res, rec)
	entityRelated := append([]string{}, related...)
	related = classifyFieldReads(e, res, rec, related)
	broke := keptStepsBroken(replayRec, rec, res.Target)
	if outside := outsideState(replayRec, replay); len(outside) > 0 {
		v.OutsideState = outside
		defer func() {
			v.Reason = strings.TrimPrefix(v.Reason+"\n"+outsideStateCaveat(res.Target, outside), "\n")
		}()
	}
	if len(blocked) > 0 && !upstreamOnly {
		defer func() {
			v.Reason = strings.TrimPrefix(v.Reason+"\n"+blockedReason(rec.RunID, blocked), "\n")
		}()
	}
	switch {
	case upstreamOnly:
		v.Outcome = sliceInconclusive
		v.Reason = blockedReason(rec.RunID, blocked)
		for _, b := range blocked {
			if !slices.Contains(v.BlockedBy, b.upstream) {
				v.BlockedBy = append(v.BlockedBy, b.upstream)
			}
		}
	case len(broke) > 0:
		v.Outcome = sliceInconclusive
		v.Reason = fmt.Sprintf("kept step(s) %s passed in source run %s and fail in the slice, so it lacks something they need", strings.Join(broke, ", "), rec.RunID)
		if len(entityRelated) > 0 {
			v.Reason += fmt.Sprintf("; dropped writes on their entities: %s", capList(entityRelated, 5))
			v.OtherDropped = slices.DeleteFunc(other, func(id string) bool { return slices.Contains(entityRelated, id) })
			v.suggestKeep(res, rec, a, entityRelated)
		} else if res.UnderIncluded {
			v.suggestKeep(res, rec, a, droppedNames(res))
		}
	case len(v.Differences) > 0 && a.otherTarget != "":
		v.Outcome = sliceInconclusive
		v.OtherTarget = a.otherTarget
		v.Reason = a.otherTarget + ": the difference can come from the target; run the chain here (shrt run " + res.SourceCommandRef() + ") and verify against -run latest"
	case len(v.Differences) > 0:
		v.Outcome = sliceNotReproduced
		note := sliceCollisionNote(e, res, replayRec)
		early := runner.EarlyRefusal(source.TokenRefused)
		switch {
		case note != "":
			v.Reason = note
		case early != nil && replay.Transport == nil:
			v.Reason = fmt.Sprintf("in source run %s step %s was refused at authentication (%s); the slice's younger token was accepted: "+
				"reproduce it with the source chain, shrt run %s", rec.RunID, res.Target, runner.TokenRefusalPhrase(*early), res.SourceCommandRef())
		case len(related) > 0:
			v.Reason = fmt.Sprintf("the slice dropped write step(s) on entities the kept steps use: %s", capList(related, 5))
			v.OtherDropped = other
			v.suggestKeep(res, rec, a, related)
		case res.UnderIncluded:
			names := droppedNames(res)
			v.Reason = fmt.Sprintf("the slice dropped write step(s): %s", capList(names, 5))
			v.suggestKeep(res, rec, a, names)
		}
		if differ := varsDifferBetween(rec, replayRec, freshSet(res)); differ != "" {
			v.Reason = strings.TrimPrefix(v.Reason+"\nthe slice ran with other vars than the source run ("+differ+"): drop the -var to use the source run's", "\n")
		}
	case driftUnseen:
		v.Outcome = sliceInconclusive
		v.Reason = fmt.Sprintf("step %s passed in source run %s and the run its drift was measured against cannot be loaded", res.Target, rec.RunID)
	case len(related) > 0:
		v.Outcome = sliceInconclusive
		v.Reason = fmt.Sprintf("the verdict matched, but the slice dropped write step(s) on entities the kept steps use: %s", capList(related, 5))
		v.OtherDropped = other
		v.suggestKeep(res, rec, a, related)
	default:
		v.Outcome = sliceReproduced
		v.Reproduced = true
		v.OtherDropped = other
		if a.otherTarget != "" {
			v.Reason = strings.TrimPrefix(v.Reason+"\n"+a.otherTarget+": the slice gave step "+res.Target+" the verdict it had there", "\n")
		}
	}
	return v, v.err()
}

func varsDifferBetween(source, replay *runner.Record, fresh map[string]bool) string {
	out := []string{}
	for _, k := range sortedKeys(replay.Vars) {
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
	if len(names) > 0 {
		v.Prove = fmt.Sprintf("shrt chain slice %s -without %s -verify -run %s", res.Source, names[len(names)-1], rec.RunID)
	}
	usable, blocked := failedInSource(rec, names)
	asked, askedBlocked := failedInSource(rec, a.keep)
	v.NotKeepable = append(append([]string{}, askedBlocked...), blocked...)
	if out := append(append([]string{}, askedBlocked...), blocked...); len(out) > 0 {
		v.Reason += fmt.Sprintf("\nleft out of next: %s did not pass in source run %s, so a slice keeping it stops there", strings.Join(out, ", "), rec.RunID)
	}
	a.keep = asked
	if len(usable) == 0 {
		v.Reason += "\nNo -keep command can reproduce this target: every dropped write it needs failed in the source run"
		return
	}
	allWrites := len(blocked) == 0 && len(usable) == len(res.DroppedWrites) && keepsEveryWriteCleanly(res, rec, a)
	if stops := stopsEarly(res, rec, a, keepList(a.keep, usable, allWrites)); len(stops) > 0 {
		v.NotKeepable = append(v.NotKeepable, stops...)
		v.Reason += fmt.Sprintf("\nno next: keeping %s also keeps %s, unanswered in source run %s, so the slice would stop there",
			strings.Join(usable, ", "), strings.Join(stops, ", "), rec.RunID)
		return
	}
	v.nextKeep = keepList(a.keep, usable, allWrites)
	v.Next = sliceVerifyCommand(res, rec.RunID, a, v.nextKeep)
}

func keepList(asked, dropped []string, allWrites bool) []string {
	keep := append([]string{}, asked...)
	if allWrites {
		if !slices.Contains(keep, chain.SliceKeepWrites) {
			keep = append(keep, chain.SliceKeepWrites)
		}
		return keep
	}
	return append(keep, dropped...)
}

func stopsEarly(res *chain.SliceResult, rec *runner.Record, a sliceVerifyArgs, keep []string) []string {
	if a.reslice == nil {
		return nil
	}
	next := a.reslice(keep)
	if next == nil {
		return nil
	}
	relaxable := relaxableIn(rec)
	out := []string{}
	for _, k := range next.Kept {
		if k.ID == res.Target {
			continue
		}
		sr, ok := rec.Step(k.ID)
		if !ok {
			continue
		}
		switch {
		case sr.Status == runner.StatusSkipped:
			out = append(out, k.ID+" (not sent)")
		case failing(sr) && !relaxable(k.ID):
			out = append(out, k.ID+" ("+sr.Status+")")
		}
	}
	return out
}

func streamedEnvelope(response any) string {
	if _, ok := chain.Get(response, chain.EnvelopePath()); ok {
		return ""
	}
	top, _ := response.(map[string]any)
	messages, _ := top[catalog.StreamMessages].([]any)
	base := ""
	for i, m := range messages {
		v, ok := chain.Get(m, chain.EnvelopePath())
		if !ok {
			continue
		}
		if base == "" || fmt.Sprint(v) != chain.EnvelopeOK() {
			base = catalog.StreamMessages + "." + strconv.Itoa(i) + "."
		}
		if fmt.Sprint(v) != chain.EnvelopeOK() {
			break
		}
	}
	return base
}

func verdictPath(sr *runner.StepRecord) string {
	if streamedEnvelope(decoded(sr.Response)) != "" {
		return catalog.StreamMessages + "[]." + chain.EnvelopePath()
	}
	return chain.EnvelopePath()
}

func verdictOf(sr *runner.StepRecord) chain.Verdict {
	return verdictIn(sr, decoded(sr.Response))
}

func verdictIn(sr *runner.StepRecord, response any) chain.Verdict {
	path := streamedEnvelope(response) + chain.EnvelopePath()
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

func compareVerdictsAt(step *chain.Step, source, replay chain.Verdict, same func(path string, a, b any) bool, other string) []string {
	alike := func(path string, a, b any) bool {
		return chain.SameClockOffset(a, b) || (same != nil && same(path, a, b))
	}
	return chain.CompareVerdictsMasking(chain.ClockRelative(step, source), chain.ClockRelative(step, replay), alike, other)
}

func clockDistanceLines(res *chain.SliceResult, source, replay chain.Verdict, same func(path string, a, b any) bool) []string {
	step, ok := res.Chain.Step(res.Target)
	if !ok || len(source.Expect) != len(replay.Expect) {
		return nil
	}
	near, far := chain.ClockRelative(step, source), chain.ClockRelative(step, replay)
	out := []string{}
	for i, e := range source.Expect {
		r := replay.Expect[i]
		if i >= len(step.Expect) || e.Path != r.Path || e.Rule != r.Rule || e.Passed || r.Passed || !chain.ClockRelativeExpectation(step.Expect[i]) {
			continue
		}
		if fmt.Sprint(e.Got) == fmt.Sprint(r.Got) {
			continue
		}
		verdict := "same distance, match"
		switch {
		case chain.SameClockOffset(near.Expect[i].Got, far.Expect[i].Got):
		case same != nil && same(e.Path, e.Got, r.Got):
			verdict = "distances differ, matched as timestamps"
		default:
			verdict = "distances differ"
		}
		out = append(out, fmt.Sprintf("compared by distance from the bound: %s %s got source %s, slice %s; distance source %s, slice %s: %s",
			e.Path, e.Rule, quoted(e.Got), quoted(r.Got), quoted(near.Expect[i].Got), quoted(far.Expect[i].Got), verdict))
	}
	return out
}

func sameUpToFixtures(source, replay map[string]any) func(path string, a, b any) bool {
	mask := func(vars map[string]any, text string) string {
		values := []string{}
		names := map[string]string{}
		for name, v := range vars {
			value := fmt.Sprint(v)
			if len(value) < 3 || value == pathmask.MaskRedacted {
				continue
			}
			values = append(values, value)
			names[value] = name
		}
		sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
		for _, value := range values {
			text = strings.ReplaceAll(text, value, "${vars."+names[value]+"}")
		}
		return text
	}
	var alike func(path string, a, b any) bool
	alike = func(path string, a, b any) bool {
		switch x := a.(type) {
		case map[string]any:
			y, ok := b.(map[string]any)
			if !ok || len(x) != len(y) {
				return false
			}
			for k, v := range x {
				w, ok := y[k]
				if !ok || !alike(pathmask.Join(path, k), v, w) {
					return false
				}
			}
			return true
		case []any:
			y, ok := b.([]any)
			if !ok || len(x) != len(y) {
				return false
			}
			for i := range x {
				if !alike(path, x[i], y[i]) {
					return false
				}
			}
			return true
		}
		if fmt.Sprint(a) == fmt.Sprint(b) || sameUpToIDs(path, a, b) {
			return true
		}
		x, ok1 := a.(string)
		y, ok2 := b.(string)
		if !ok1 || !ok2 {
			return false
		}
		mx, my := mask(source, x), mask(replay, y)
		return mx != x && mx == my
	}
	return alike
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
	relaxable := relaxableIn(rec)
	next := a.reslice(append(append([]string{}, a.keep...), chain.SliceKeepWrites))
	if next == nil || len(next.DroppedWrites) > 0 {
		return false
	}
	for _, k := range next.Kept {
		if k.ID == res.Target {
			continue
		}
		if sr, ok := rec.Step(k.ID); ok && failing(sr) && !relaxable(k.ID) {
			return false
		}
	}
	return true
}

func sliceVerifyCommand(res *chain.SliceResult, runID string, a sliceVerifyArgs, keep []string) string {
	parts := []string{"shrt chain slice", res.SourceCommandRef(), "-step", res.Target}
	parts = append(parts, "-run", runID)
	if len(keep) > 0 {
		parts = append(parts, "-keep", strings.Join(keep, ","))
	}
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

func mayOverwriteSlice(path string, res *chain.SliceResult, source, verify bool) error {
	if _, err := os.Stat(path); err != nil || source {
		return nil
	}
	existing, err := chain.LoadFile(path)
	if err != nil || existing.Name != res.Chain.Name ||
		!strings.HasPrefix(existing.Description, chain.SliceDescriptionPrefix(res.Source, res.Target)) {
		return fmt.Errorf("%s already exists and is not a slice of %s for step %s: name another file, -write <name>",
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
	return fmt.Errorf("%s holds a VERIFIED slice that differs from this one: add -verify to verify it in its place, or name another file, -write <name>", path)
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
			return chain.RefusedNotSent, true
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
	return fmt.Errorf("the slice reads var(s) %s, which %s does not declare: pass %s",
		strings.Join(res.MissingVars, ", "), res.Source, strings.Join(flags, " "))
}

func reachedStep(rec *runner.Record, step string) (bool, string) {
	sr, ok := rec.Step(step)
	if !ok {
		return false, "not in run, " + rec.Status
	}
	if sr.Status == runner.StatusPassed || sr.Status == runner.StatusFailed {
		return true, sr.Status
	}
	if sr.Status == runner.StatusError && (sr.HTTPStatus != 0 || len(sr.Response) > 0 || sr.Transport != nil) {
		return true, sr.Status
	}
	return false, sr.Status
}

func newestRunReaching(e *env, chainName, step, skip string) (*runner.Record, error) {
	rec, err := newestRecordReaching(e, chainName, step, skip, false)
	if rec != nil || err != nil {
		return rec, err
	}
	return newestRecordReaching(e, chainName, step, skip, true)
}

func newestRecordReaching(e *env, chainName, step, skip string, replays bool) (*runner.Record, error) {
	ids, err := e.store.ListRuns(chainName)
	if err != nil {
		return nil, err
	}
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] == skip {
			continue
		}
		rec, err := e.store.LoadRun(chainName, ids[i])
		if err != nil || (rec.ReplayOf != "") != replays {
			continue
		}
		if ok, _ := reachedStep(rec, step); ok || step == "" {
			return rec, nil
		}
	}
	return nil, nil
}

func latestRun(e *env, chainName, step string) (*runner.Record, error) {
	ids, err := e.store.ListRuns(chainName)
	if err != nil {
		return nil, err
	}
	latest, err := e.store.LatestRun(chainName)
	if err != nil || latest.ReplayOf == "" {
		return latest, err
	}
	own, err := newestRecordReaching(e, chainName, "", "", false)
	if err != nil || own == nil {
		return latest, err
	}
	picked := "note: -run latest: verify replay " + latest.RunID
	since := ", made after the safe spot's approval"
	if spot, err := e.store.LoadSafeSpot(chainName); err == nil && !own.StartedAt.After(spot.ConfirmedAt) {
		since = ""
	}
	if step == "" && !slices.Equal(failedSteps(latest), failedSteps(own)) {
		if since != "" {
			picked += fmt.Sprintf(", in which %s; in shrt run %s%s, %s", failedCount(latest), own.RunID, since, failedCount(own))
		}
		fmt.Fprintln(os.Stderr, picked)
		return latest, nil
	}
	ownReached, _ := reachedStep(own, step)
	if latestReached, _ := reachedStep(latest, step); ownReached != latestReached {
		return latest, nil
	}
	moved := func(s string) bool { return s == runner.StatusFailed || s == "drifted" }
	if l, o := movedStatus(e, latest, step), movedStatus(e, own, step); moved(l) != moved(o) {
		if since != "" {
			picked += fmt.Sprintf(", in which %s %s; in shrt run %s%s, it %s", step, l, own.RunID, since, o)
		}
		fmt.Fprintln(os.Stderr, picked)
		return latest, nil
	}
	prev, err := e.store.LoadRun(chainName, ids[len(ids)-2])
	if err != nil || !replayBesideRun(prev, latest) {
		fmt.Fprintln(os.Stderr, picked)
		return latest, nil
	}
	fmt.Fprintf(os.Stderr, "note: -run latest: shrt run %s\n", own.RunID)
	return own, nil
}

func failedCount(rec *runner.Record) string {
	switch n := len(failedSteps(rec)); n {
	case 0:
		return "no step failed"
	case 1:
		return "1 step failed"
	default:
		return fmt.Sprintf("%d steps failed", n)
	}
}

func newerFailing(e *env, rec *runner.Record) *runner.Record {
	ids, err := e.store.ListRuns(rec.Chain)
	if err != nil {
		return nil
	}
	for i := len(ids) - 1; i >= 0 && ids[i] != rec.RunID; i-- {
		if other, err := e.store.LoadRun(rec.Chain, ids[i]); err == nil && len(failedSteps(other)) > 0 {
			return other
		}
	}
	return nil
}

func stepStatus(rec *runner.Record, step string) string {
	if sr, ok := rec.Step(step); ok {
		return sr.Status
	}
	return "was not run"
}

func loadRunReaching(e *env, chainName, ref, runID, step string) (*runner.Record, error) {
	if runID == "latest" {
		rec, err := latestRun(e, chainName, step)
		if err != nil {
			return nil, err
		}
		if ok, why := reachedStep(rec, step); !ok {
			instead := ""
			if failed := failedSteps(rec); len(failed) > 0 && failed[0] != step {
				instead = fmt.Sprintf("slice its first failing step instead (shrt chain slice %s -step %s -run %s), or ", ref, failed[0], rec.RunID)
			}
			earlier := "<id> of an earlier run that did"
			if other, err := newestRunReaching(e, chainName, step, rec.RunID); err == nil && other != nil {
				earlier = other.RunID + ", an earlier run that did"
			}
			return nil, exitWith(3, "-run latest is run %s, which did not evaluate step %s (%s): %spass -run %s", rec.RunID, step, why, instead, earlier)
		}
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
	msg := fmt.Sprintf("run %s did not reach step %q (%s), so it has no verdict to compare", rec.RunID, step, why)
	other, err := newestRunReaching(e, chainName, step, rec.RunID)
	if err != nil {
		return nil, err
	}
	if other == nil {
		return nil, fmt.Errorf("%s, and no recorded run of %s reached it: shrt run %s", msg, chainName, ref)
	}
	return nil, fmt.Errorf("%s; run %s did: pass -run %s", msg, other.RunID, other.RunID)
}

func freshVarsError(res *chain.SliceResult, source *chain.Chain, rec *runner.Record, supplied varFlags) error {
	reused := []string{}
	from := []string{}
	for _, name := range res.FreshVars {
		if v, given := supplied[name]; given {
			if rec == nil || rec.Vars[name] == nil || fmt.Sprint(rec.Vars[name]) == pathmask.MaskRedacted || fmt.Sprint(v) != fmt.Sprint(rec.Vars[name]) {
				continue
			}
			reused = append(reused, name)
			from = append(from, fmt.Sprintf("%s=%v (-var %s=%v is the value run %s used)", name, v, name, v, rec.RunID))
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
	return fmt.Errorf("kept writes create with %s, which already exists (%s): pass %s, a value this backend has not seen",
		strings.Join(reused, ", "), strings.Join(from, "; "), freshFlags(reused))
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
	relaxable := relaxableIn(rec)
	for _, id := range ids {
		sr, ok := rec.Step(id)
		if ok && failing(sr) && !relaxable(id) {
			blocked = append(blocked, id+" ("+sr.Status+")")
			continue
		}
		usable = append(usable, id)
	}
	return usable, blocked
}

func sliceDir(e *env, c *chain.Chain) string {
	chains := e.chainsDir()
	if c.SourcePath == "" {
		return chains
	}
	dir, err := filepath.Abs(filepath.Dir(c.SourcePath))
	if err != nil {
		return chains
	}
	if abs, err := filepath.Abs(chains); err == nil && abs == dir {
		return chains
	}
	return dir
}

func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	x, err1 := filepath.Abs(a)
	y, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	if x == y {
		return true
	}
	sx, err1 := os.Stat(x)
	sy, err2 := os.Stat(y)
	return err1 == nil && err2 == nil && os.SameFile(sx, sy)
}

func sliceChainRef(arg string, c *chain.Chain) string {
	if isSlicePath(arg) {
		return filepath.ToSlash(arg)
	}
	return c.Name
}
