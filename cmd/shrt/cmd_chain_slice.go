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

const sliceUsage = "usage: shrt chain slice <chain> -step <id> [flags]\n" +
	"       shrt chain slice <chain> -without <id,...|failed> [-run <id>] [-write [<name>]]"

const sliceExitCodes = "\nexit codes (plain slice):\n" +
	"  0  the slice was printed or written\n" +
	"  1  refused: unknown chain, step or run, -mode pin without -run, a file -write would overwrite\n" +
	"  3  -run latest did not evaluate the step\n" +
	"exit codes (-verify):\n" +
	"  0  reproduced\n" +
	"  1  NOT REPRODUCED, or a flag that cannot be parsed\n" +
	"  2  DID NOT RUN: the step was never answered, or -verify refused before sending\n" +
	"  3  INCONCLUSIVE: the slice dropped a write a kept step needs, or another target\n" +
	"  4  intermittent: reproduced k/N of the -repeat runs\n"

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
	step := fs.String("step", "", "step `id` the slice must reproduce")
	mode := fs.String("mode", chain.SliceModeClosure, "closure (rebuild producers) or pin (values from the run)")
	runID := fs.String("run", "", "run record `id` or latest, required by -mode pin and -verify")
	verify := fs.Bool("verify", false, "run the slice and compare the step's verdict with the run record")
	resend := fs.Bool("resend-writes", false, "with -verify -mode pin, send a kept write on an entity the source run created")
	asJSON := fs.Bool("json", false, "print JSON")
	build := fs.String("build", "", "with -verify, stamp this build `id` into the run record")
	force := fs.Bool("force", false, "with -write, overwrite a file that is not this slice")
	verbose := fs.Bool("v", false, "also print each kept step and why, the vars and values written, and the dropped writes")
	vars := varFlags{}
	fs.Var(vars, "var", "set a var as `key=value`, repeatable")
	write := &optionalString{}
	fs.Var(write, "write", "write .shrt/chains/`[name]`.yaml (default <chain>-slice-<step>), or a path with a slash")
	repeat := fs.Int("repeat", 3, "with -verify, run the slice this many times")
	keptRed := &keptRedFlag{}
	fs.Var(keptRed, "kept-red", "pin kept_red on the step's failed expectations; =`id[,id]` also pins those steps")
	without := &stepList{}
	fs.Var(without, "without", "leave out these steps and those reading them: `id[,id]|failed`")
	keep := &stepList{}
	fs.Var(keep, "keep", "also keep these earlier `id[,id]` steps; writes keeps every earlier write")
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
	for _, v := range append([]string{name}, rest[1:]...) {
		if strings.HasPrefix(v, "=") {
			named := "-write"
			if keptRed.on && len(keptRed.steps) == 0 {
				named = "-kept-red"
			}
			return fmt.Errorf("unexpected argument %q: did you mean %s%s (no space before the equals sign)?", v, named, v)
		}
	}
	if len(rest) == 2 {
		if !write.set || name != "" {
			if keptRed.on {
				return fmt.Errorf("unexpected argument %q: -kept-red takes its steps after an equals sign, -kept-red=%s\n\nusage: shrt chain slice <chain> -step <step-id> [-kept-red[=<id,...>]] [-write [<name>]]", rest[1], rest[1])
			}
			return fmt.Errorf("unexpected argument %q\n\nusage: shrt chain slice <chain> -step <step-id> [-write [<name>]]", rest[1])
		}
		name = rest[1]
	}
	if len(*without) > 0 {
		if *step != "" || *verify || keptRed.on || len(*keep) > 0 || *mode != chain.SliceModeClosure {
			return fmt.Errorf("-without writes the chain minus some steps, not a slice: it takes no -step, -verify, -kept-red, -keep or -mode")
		}
		return sliceWithout(rest[0], *without, *runID, write, name, *force, *asJSON)
	}
	if *step == "" {
		return fmt.Errorf("-step is required: name the step the slice must reproduce")
	}
	keptRed.steps = slices.DeleteFunc(keptRed.steps, func(id string) bool { return id == *step })
	if keptRed.on && *runID == "" {
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

	opts := chain.SliceOptions{Mode: *mode, Name: name, RPCOf: rpcOf(e), Keep: append(append([]string{}, *keep...), keptRed.steps...), Pinned: keptRed.steps, Vars: vars, IsLogin: isLoginStep(e)}
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
		needStep := *mode == chain.SliceModePin || *verify || keptRed.on
		if _, known := c.Step(*step); !known {
			needStep = false
		}
		if needStep {
			rec, err = loadRunReaching(e, c.Name, ref, *runID, *step)
		} else if *runID == "latest" {
			rec, err = latestRun(e, c.Name, *step)
		} else {
			rec, err = e.store.LoadRun(c.Name, *runID)
		}
		if err != nil {
			return err
		}
		if err := foreignSourceRun(e, rec, ref, *mode, *verify); err != nil {
			return err
		}
		opts.RunID = rec.RunID
		opts.Value = recordValues(rec)
		opts.RunVars = recordVars(rec)
		opts.RunVarsAsDefaults = *verify
		opts.Refused = refusedIn(rec)
		opts.Performed = performedIn(rec)
		if !keptRed.on {
			opts.Relax = relaxIn(rec)
		}
		opts.StateIrrelevant = stateIrrelevantIn(e, lib, c, rec)
	}

	res, err := chain.Slice(c, *step, opts)
	if err != nil {
		return err
	}
	res.SourceRef = ref
	redPins, pinned := []chain.Pin{}, []chain.Pin{}
	inherited, pinRun := len(res.Chain.KeptRed), ""
	if rec != nil {
		pinRun = rec.RunID
	}
	if keptRed.on {
		if redPins, err = failurePins(res.Chain, rec, *step); err != nil {
			return readsFailedAfter(e, ref, rec, *step, err)
		}
		stepPins, err := keptStepPins(res, rec, keptRed.steps)
		if err != nil {
			return err
		}
		res.Chain.KeptRed = append(res.Chain.KeptRed, stepPins...)
		pinned = stepPins
		if !*verify {
			pinned = append(pinned, redPins...)
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
		if pinned := pinnedWrites(res); len(pinned) > 0 && !*resend {
			return pinnedWritesError(res, pinned, closureCommand(c, *step, opts, res, vars))
		}
	}

	whole := write.set && name == "" && len(res.Kept) == res.Total && c.SourcePath != "" && !keptRed.on
	if whole {
		res.Chain.Name = c.Name
	}
	written, created := "", false
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
		path := filepath.Join(sliceDir(e, c), res.Chain.Name+".yaml")
		if writePath != "" {
			path = writePath
		}
		source := writePath != "" && sameSliceFile(path, c.SourcePath)
		if source {
			res.Chain.Name = c.Name
		}
		if err := mayOverwriteSlice(path, res, *force || source, *verify); err != nil {
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
			vars: vars, quiet: *asJSON, verbose: *verbose, persist: write.set, name: writeArg, keep: *keep, build: *build, keptRed: keptRed.arg(),
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
		}, repeatCount(fs, *repeat, res))
		if verdict == nil {
			return verifyErr
		}
		p.sent = true
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
		case rerun && (verdict.Outcome == sliceNotReproduced || verdict.Outcome == sliceInconclusive || verdict.Outcome == sliceIntermittent):
			outcome, detail := "not reproduced", first
			switch verdict.Outcome {
			case sliceInconclusive:
				outcome, detail = "INCONCLUSIVE", why
			case sliceIntermittent:
				outcome, detail = fmt.Sprintf("INTERMITTENT, reproduced %d/%d", verdict.ReproducedRuns, verdict.Repeat), ""
			}
			line := res.OwnRunOutcome(outcome, c.Name, verdict.SourceRun, verdict.runsLabel(), now, detail)
			if err := recordVerdictIn(c.SourcePath, func(d string) string { return chain.RecordRerun(d, line) }); err != nil {
				return err
			}
			verdict.Recorded = c.SourcePath
		case verdict.Outcome == sliceReproduced:
			res.MarkReproduced(verdict.SourceRun, verdict.runsLabel(), now)
		case verdict.Outcome == sliceNotReproduced && !whole:
			res.MarkNotReproduced(verdict.SourceRun, verdict.runsLabel(), now, first)
		case verdict.Outcome == sliceInconclusive && !whole:
			res.MarkInconclusive(verdict.SourceRun, verdict.runsLabel(), now, why)
		case verdict.Outcome == sliceIntermittent && !whole:
			res.MarkIntermittent(verdict.SourceRun, verdict.runsLabel(), verdict.ReproducedRuns, verdict.Repeat, now)
		}
		if keptRed.on && verdict.Outcome == sliceReproduced && !whole {
			if verdict.replay != nil {
				res.Chain.KeptRed = res.Chain.KeptRed[:inherited]
				pinned, pinRun = replayPins(res, verdict.replay), verdict.replay.RunID
				res.Chain.KeptRed = append(res.Chain.KeptRed, pinned...)
			} else {
				pinned = append(pinned, redPins...)
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
	printSlice(res, written, verdict, *verbose)
	if len(pinned) > 0 && (verdict == nil || verdict.Outcome == sliceReproduced) {
		fmt.Print(keptRedLine(c, ref, rec, pinRun, res.Chain, pinned, written))
		if verdict == nil {
			fmt.Println("unverified: add -verify to run the slice before pinning; a slice that lost a dependency on state shows the defect gone")
		}
	} else if keptRed.on && verdict != nil {
		follow := ""
		if verdict.Next != "" {
			follow = "follow the next: line above, or "
		}
		fmt.Printf("\nkept_red: NOT pinned, since the slice did not reproduce %s's verdict in run %s; a slice that does not show the defect "+
			"cannot be kept red on it, so it was not written either: %spin the defect in %s itself\n", *step, rec.RunID, follow, c.Name)
	}
	if pinned := pinnedWrites(res); len(pinned) > 0 && verdict == nil && !*resend {
		fmt.Printf("\nWARNING: running this slice re-sends write step(s) on what run %s created: %s.\n"+
			"  That run already did those writes, so the send changes live entities a second time and answers for a second\n"+
			"  write, not the first. To reproduce a write, slice in closure mode: %s\n",
			res.Run, strings.Join(pinned, "; "), closureCommand(c, *step, opts, res, vars))
	}
	if written != "" && !sameSliceFile(written, c.SourcePath) && len(res.Chain.KeptRed) == 0 {
		fmt.Print(sweepNote(e, written, res.Chain.Name))
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

const hypothesisLine = "this slice is a HYPOTHESIS until it is run: a dependency that is state rather than a reference leaves no trace in the YAML"

func printSliceHeader(res *chain.SliceResult) {
	fmt.Printf("slice of %s for step %s, mode %s", res.Source, res.Target, res.Mode)
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
	if len(res.Unmet) > 0 {
		fmt.Println("\nunmet prerequisites, no earlier step calls them, so the slice may not stand alone:")
		for _, u := range res.Unmet {
			fmt.Printf("  %s %s (declared for %s)\n", u.Edge, u.RPC, u.Step)
		}
	}
	if len(res.Relaxed) > 0 {
		fmt.Printf("\nrelaxed: kept step(s) failed these expectations in run %s after the backend answered, so the call took effect;\n"+
			"the slice drops them so it reaches the target (each call must still be answered):\n", res.Run)
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
	undeclared, fromRun, fromFlag := []chain.FilledVar{}, []chain.FilledVar{}, []chain.FilledVar{}
	for _, f := range res.FilledVars {
		switch {
		case f.Declared && f.From == chain.VarFromFlag:
			fromFlag = append(fromFlag, f)
		case f.Declared:
			fromRun = append(fromRun, f)
		default:
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
	if len(fromFlag) > 0 {
		fmt.Println("\nvars written with the -var value given, not the chain's default, which the chain's own runs already created with:")
		for _, f := range fromFlag {
			fmt.Printf("  %s = %v  (default %v)\n", f.Var, f.Value, f.Default)
		}
	}
	if len(fromRun) > 0 {
		fmt.Printf("\nvars written with the value run %s used, not the chain's default, so the slice sends what that run sent:\n", res.Run)
		for _, f := range fromRun {
			fmt.Printf("  %s = %v  (default %v)\n", f.Var, f.Value, f.Default)
		}
	}
	if len(res.Satisfied) > 0 {
		fmt.Printf("\ncontract prerequisites run %s already performed, left to that run and not re-sent (pin mode reproduces\n"+
			"the state that run left; re-sending a write would change it):\n", res.Run)
		for _, sat := range res.Satisfied {
			fmt.Printf("  %4d  %s  %s %s (declared for %s)\n", sat.Index, sat.ID, sat.Edge, sat.RPC, sat.For)
		}
	}
	if len(res.DroppedWrites) > 0 {
		fmt.Println("\ndropped write steps:")
		for _, d := range res.DroppedWrites {
			fmt.Printf("    %4d  %s  %s\n", d.Index, d.ID, shortCall(d.Call))
		}
	}
	if len(res.RefusedWrites) > 0 {
		fmt.Printf("\n%d dropped write step(s) were refused in run %s and act on no entity a kept step uses, so they do not count toward under-inclusion:\n", len(res.RefusedWrites), res.Run)
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
		line += ": WARNING possible under-inclusion, a kept step may depend on state one of them left without referencing it, so the slice can be too small and still go green"
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
	case carried > 0 && len(dropped) == 0:
		fmt.Printf("\nkept_red: the slice carries all %d pin(s) of %s, so it is kept red on the same defect\n", carried, res.Source)
	case carried > 0:
		fmt.Printf("\nkept_red: the slice carries %d pin(s) of %s, on the steps it keeps, and drops %d on steps it does not keep: %s\n",
			carried, res.Source, len(dropped), strings.Join(dropped, ", "))
	default:
		fmt.Printf("\nkept_red: %s pins only steps this slice does not keep (%s), so the slice carries no kept_red: if it fails, "+
			"it fails every run of it, and every gate that runs it\n", res.Source, strings.Join(dropped, ", "))
	}
}

func sweepNote(e *env, written, name string) string {
	shown := shownPath(written)
	chains := shownPath(e.chainsDir())
	if filepath.Dir(written) != filepath.Clean(e.chainsDir()) {
		return fmt.Sprintf("note: %s is outside %s, so no sweep reads it; run it by path: shrt run %s\n", shown, chains, shown)
	}
	return fmt.Sprintf("note: %s is in %s, so lint, hollow and the gate run it; to keep it out: mkdir -p .shrt/scratch && mv %s .shrt/scratch/\n",
		shown, chains, shown)
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
	NotKeepable    []string          `json:"not_keepable,omitempty"`
	OtherDropped   []string          `json:"dropped_writes_other_entities,omitempty"`
	Repeat         int               `json:"repeat,omitempty"`
	ReproducedRuns int               `json:"reproduced_runs,omitempty"`
	Runs           []sliceRunOutcome `json:"runs,omitempty"`
	OtherTarget    string            `json:"source_target_differs,omitempty"`
	BlockedBy      []string          `json:"unevaluated_behind,omitempty"`
	Recorded       string            `json:"verdict_written_to,omitempty"`
	OutsideState   []string          `json:"outside_state,omitempty"`
	ByDistance     []string          `json:"compared_by_distance,omitempty"`
	SourceReplay   string            `json:"source_replay_of,omitempty"`
	replay         *runner.Record
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
	keptRed string
	vars    varFlags
	quiet   bool
	verbose bool
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

func foreignSourceRun(e *env, rec *runner.Record, ref, mode string, verify bool) error {
	where := sourceTargetDiffers(e, rec)
	if where == "" || mode != chain.SliceModePin {
		return nil
	}
	msg := fmt.Sprintf("%s: -mode pin would send the ids and values run %s was given there, which this target never issued,\n"+
		"so its answer (a not-found) would say nothing about step behaviour. Slice with -mode closure, which rebuilds every\n"+
		"producer on this target, or run the chain here (shrt run %s) and pin from that run: -run latest", where, rec.RunID, ref)
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
		sliceIntermittent:  "intermittent: reproduced",
	}[v.Outcome] + v.countLabel()
	slice := v.SliceRun
	if slice == "" {
		slice = "none"
	}
	source := v.SourceRun
	if v.SourceReplay != "" {
		source += " (a shrt verify replay)"
	}
	if v.Repeat > 1 {
		fmt.Fprintf(&b, "verify %s: step %s, source run %s, %d slice runs, details from slice run %s", head, v.Step, source, v.Repeat, slice)
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
		for _, line := range v.ByDistance {
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
	if text, ok := v.(string); ok && (text == "" || strings.TrimSpace(text) != text) {
		return strconv.Quote(text)
	}
	if v == nil {
		return "(absent)"
	}
	switch v.(type) {
	case map[string]any, []any:
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err == nil {
			return strings.TrimRight(b.String(), "\n")
		}
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
	case sliceIntermittent:
		return exitWith(4, "step %s: intermittent, the slice reproduced it in %d of %d runs", v.Step, v.ReproducedRuns, v.Repeat)
	case sliceNotReproduced:
		return exitWith(1, "step %s was NOT reproduced by the slice", v.Step)
	case sliceDidNotRun:
		return exitWith(2, "the slice DID NOT RUN step %s, so nothing was verified", v.Step)
	case sliceInconclusive:
		if v.OtherTarget != "" {
			return exitWith(3, "step %s: INCONCLUSIVE, %s", v.Step, v.OtherTarget)
		}
		if len(v.BlockedBy) > 0 {
			return exitWith(3, "step %s: INCONCLUSIVE, its failing expectations were not evaluated in the source run, behind %s", v.Step, strings.Join(v.BlockedBy, ", "))
		}
		return exitWith(3, "step %s: INCONCLUSIVE, the verdict matched but the slice dropped write step(s) acting on entities the kept steps use", v.Step)
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
		SourceReplay: rec.ReplayOf,
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
	v.replay = replayRec
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
		if stop := stoppedAsInSource(replayRec, rec); stop != "" && a.keptRed != "" {
			named := strings.TrimPrefix(strings.TrimPrefix(a.keptRed, "-kept-red"), "=")
			a.keptRed = "-kept-red=" + strings.TrimPrefix(named+","+stop, ",")
			v.Reason += fmt.Sprintf("\nStep %s failed in source run %s too; pin it as well, so the slice runs on to %s", stop, rec.RunID, res.Target)
			v.Next = sliceVerifyCommand(res, rec.RunID, a, a.keep)
			return v, v.err()
		}
		if at := stoppedWhereSourcePassed(replayRec, rec); at != "" {
			if related, other := relatedDroppedWrites(res, rec); len(related) > 0 {
				v.Reason += fmt.Sprintf("\nStep %s passed in source run %s and fails here, so it likely reads state a dropped write built:\n"+
					"%s act on entities the kept steps use.", at, rec.RunID, capList(related, 5))
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
	v.Differences = compareSliceVerdicts(res, v.Source, v.Replay, sameUpToFixtures(rec.Vars, replayRec.Vars))
	v.ByDistance = clockDistanceLines(res, v.Source, v.Replay, sameUpToFixtures(rec.Vars, replayRec.Vars))
	blocked := blockedReads(v.Source, v.Replay)
	upstreamOnly := false
	if evaluatedBlocked(blocked) {
		v.Differences = compareSliceVerdicts(res, v.Source, withoutBlocked(v.Source, v.Replay, blocked), sameUpToFixtures(rec.Vars, replayRec.Vars))
		upstreamOnly = len(v.Differences) == 0
	}
	related, other := relatedDroppedWrites(res, rec)
	entityRelated := append([]string{}, related...)
	related, reads := classifyFieldReads(e, res, rec, related)
	broke := keptStepsBroken(replayRec, rec, res.Target)
	uncreated := uncreatedExpected(res, source)
	if outside := outsideState(replayRec, replay); len(outside) > 0 {
		v.OutsideState = outside
		defer func() {
			v.Reason = strings.TrimPrefix(v.Reason+"\n"+outsideStateCaveat(res.Target, outside), "\n")
		}()
	}
	defer func() {
		for _, r := range reads {
			v.Reason = strings.TrimPrefix(v.Reason+"\ninfo: "+r.note(), "\n")
		}
	}()
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
		v.Reason = fmt.Sprintf("kept step(s) %s passed in source run %s and fail in the slice, so the slice lacks something they need\n"+
			"and the verdict of %s is no receipt.", strings.Join(broke, ", "), rec.RunID, res.Target)
		if len(entityRelated) > 0 {
			v.Reason += fmt.Sprintf(" Dropped write step(s) acting on entities the kept steps use: %s. Keep them and verify again.",
				capList(entityRelated, 5))
			v.OtherDropped = slices.DeleteFunc(other, func(id string) bool { return slices.Contains(entityRelated, id) })
			v.suggestKeep(res, rec, a, entityRelated)
		} else if res.UnderIncluded {
			v.suggestKeep(res, rec, a, droppedNames(res))
		}
		reads = nil
	case len(v.Differences) == 0 && len(uncreated) > 0:
		v.Outcome = sliceInconclusive
		v.Reason = fmt.Sprintf("the verdict matched, but a failing expectation of step %s compares with what dropped write step(s) %s\n"+
			"created in source run %s, and the slice never sends them: what they created is absent from this slice whatever the\n"+
			"backend does, so a correct backend fails it the same way and the match is no receipt. Keep them and verify again.",
			res.Target, strings.Join(uncreated, ", "), rec.RunID)
		keep := append([]string{}, uncreated...)
		for _, r := range related {
			if !slices.Contains(keep, r) {
				keep = append(keep, r)
			}
		}
		other = slices.DeleteFunc(other, func(id string) bool { return slices.Contains(keep, id) })
		v.OtherDropped = other
		v.suggestKeep(res, rec, a, keep)
	case len(v.Differences) > 0 && a.otherTarget != "":
		v.Outcome = sliceInconclusive
		v.OtherTarget = a.otherTarget
		v.Reason = a.otherTarget + ": the verdict differs, and the difference can come from the target (its data, build\n" +
			"or configuration) rather than from what the slice left out. Run the chain on this target (shrt run " + res.SourceCommandRef() + ")\n" +
			"and verify the slice against that run: -run latest"
	case len(v.Differences) > 0:
		v.Outcome = sliceNotReproduced
		note := sliceCollisionNote(e, res, replayRec)
		early := runner.EarlyRefusal(source.TokenRefused)
		switch {
		case note != "":
			v.Reason = note
		case early != nil && replay.Transport == nil:
			v.Reason = fmt.Sprintf("in source run %s step %s was refused at authentication: the %s. The slice sent it with a "+
				"younger token, which was accepted. The refusal depends on how long the session had lived, which no slice carries "+
				"and no dropped step explains: reproduce it with the source chain (shrt run %s), where it is reported as a "+
				"finding when it repeats", rec.RunID, res.Target,
				strings.Replace(runner.TokenRefusalPhrase(*early), "token refused", "token was refused", 1), res.SourceCommandRef())
		case len(related) > 0:
			v.Reason = fmt.Sprintf("the slice dropped %d write step(s) that act on entities the kept steps use: %s.\n"+
				"The difference can come from state those writes would have built. Keep them and verify again\n"+
				"against the same source run.", len(related), capList(related, 5))
			if note := otherEntitiesNote(other); note != "" {
				v.Reason += "\nNot suggested: " + note + "."
			}
			v.OtherDropped = other
			v.suggestKeep(res, rec, a, related)
		case res.UnderIncluded:
			names := droppedNames(res)
			v.Reason = fmt.Sprintf("the slice dropped %d write step(s): %s.\n"+
				"The difference can come from state those writes would have built. Keep them and verify again\n"+
				"against the same source run; if the verdict then matches, drop ids from -keep to find the one\n"+
				"the target needs.", len(names), capList(names, 5))
			v.suggestKeep(res, rec, a, names)
		}
		if differ := varsDifferBetween(rec, replayRec, freshSet(res)); differ != "" {
			v.Reason = strings.TrimPrefix(v.Reason+"\nthe slice ran with other vars than the source run ("+differ+
				"), so a value it sent or asserted can differ for that reason alone: drop the -var to use the source run's", "\n")
		}
	case len(related) > 0:
		v.Outcome = sliceInconclusive
		v.Reason = fmt.Sprintf("the verdict matched, but the slice dropped %d write step(s) that act on entities the kept steps use: %s.\n"+
			"Those writes can have side effects on those entities that no reference in the chain declares, so a\n"+
			"match is not evidence that the slice reproduces the failure. Keep them and verify again against the same source run.", len(related), capList(related, 5))
		if note := otherEntitiesNote(other); note != "" {
			v.Reason += "\nNot suggested: " + note + "."
		}
		v.OtherDropped = other
		v.suggestKeep(res, rec, a, related)
	default:
		v.Outcome = sliceReproduced
		v.Reproduced = true
		v.OtherDropped = other
		if note := otherEntitiesNote(other); note != "" {
			v.Reason = "info: " + note + ", and the verdict matched without them"
		}
		if a.otherTarget != "" {
			v.Reason = strings.TrimPrefix(v.Reason+"\n"+a.otherTarget+": the slice gave step "+res.Target+" the verdict it had there", "\n")
		}
	}
	return v, v.err()
}

func uncreatedExpected(res *chain.SliceResult, source *runner.StepRecord) []string {
	step, ok := res.Chain.Step(res.Target)
	if !ok || source == nil {
		return nil
	}
	dropped := map[string]bool{}
	for _, d := range res.DroppedWrites {
		dropped[d.ID] = true
	}
	out := []string{}
	for i, r := range source.Expect {
		if r.Passed || i >= len(step.Expect) {
			continue
		}
		text := fmt.Sprint(step.Expect[i].Operands()...)
		for _, p := range res.Pins {
			if dropped[p.Producer] && strings.Contains(text, "${vars."+p.Var+"}") && !slices.Contains(out, p.Producer) {
				out = append(out, p.Producer)
			}
		}
	}
	return out
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
	allWrites := len(blocked) == 0 && len(usable) == len(res.DroppedWrites) && keepsEveryWriteCleanly(res, rec, a)
	if stops := stopsEarly(res, rec, a, keepList(a.keep, usable, allWrites)); len(stops) > 0 {
		v.NotKeepable = append(v.NotKeepable, stops...)
		v.Reason += fmt.Sprintf("\nNo next: the slice that keeps %s also keeps %s, which did not get an answer it can pass in source run %s\n"+
			"(a relaxed expectation needs an answered call), so it would stop there before step %s and could only give DID NOT RUN.\n"+
			"Fix that step, or write a chain that reaches the target without it, run it, and verify a slice of that.",
			strings.Join(usable, ", "), strings.Join(stops, ", "), rec.RunID, res.Target)
		return
	}
	v.Next = keepWritesCommand(res, rec.RunID, a, usable, allWrites)
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
		case (sr.Status == runner.StatusFailed || sr.Status == runner.StatusError) && !relaxable(k.ID):
			out = append(out, k.ID+" ("+sr.Status+")")
		}
	}
	return out
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

func compareSliceVerdicts(res *chain.SliceResult, source, replay chain.Verdict, same func(path string, a, b any) bool) []string {
	step, _ := res.Chain.Step(res.Target)
	alike := func(path string, a, b any) bool {
		return chain.SameClockOffset(a, b) || (same != nil && same(path, a, b))
	}
	return chain.CompareVerdictsMasking(chain.ClockRelative(step, source), chain.ClockRelative(step, replay), alike)
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
		verdict := "the same distance, so they match"
		switch {
		case chain.SameClockOffset(near.Expect[i].Got, far.Expect[i].Got):
		case same != nil && same(e.Path, e.Got, r.Got):
			verdict = "the distances differ, and both values are timestamp-shaped, which differ every run, so they were matched as timestamps, not by distance"
		default:
			verdict = "the distances differ, so they differ"
		}
		out = append(out, fmt.Sprintf("compared by distance from the bound: %s %s reads the clock, so the got values differ "+
			"(source %s, slice %s) and each is compared by its distance from the bound its own run computed (source %s, slice %s): %s",
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
		if sr, ok := rec.Step(k.ID); ok && (sr.Status == runner.StatusFailed || sr.Status == runner.StatusError) && !relaxable(k.ID) {
			return false
		}
	}
	return true
}

func keepWritesCommand(res *chain.SliceResult, runID string, a sliceVerifyArgs, dropped []string, allWrites bool) string {
	return sliceVerifyCommand(res, runID, a, keepList(a.keep, dropped, allWrites))
}

func sliceVerifyCommand(res *chain.SliceResult, runID string, a sliceVerifyArgs, keep []string) string {
	parts := []string{"shrt chain slice", res.SourceCommandRef(), "-step", res.Target}
	if res.Mode != chain.SliceModeClosure {
		parts = append(parts, "-mode", res.Mode)
	}
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
	if a.keptRed != "" {
		parts = append(parts, a.keptRed)
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
	ownReached, _ := reachedStep(own, step)
	if latestReached, _ := reachedStep(latest, step); ownReached != latestReached {
		return latest, nil
	}
	if stepFailed(latest, step) != stepFailed(own, step) {
		fmt.Fprintf(os.Stderr, "note: -run latest is run %s, the newest record, a `shrt verify` replay in which %s %s; the newest `shrt run` record, %s, %s it\n",
			latest.RunID, step, stepStatus(latest, step), own.RunID, stepStatus(own, step))
		return latest, nil
	}
	prev, err := e.store.LoadRun(chainName, ids[len(ids)-2])
	if err != nil || !replayBesideRun(prev, latest) {
		fmt.Fprintf(os.Stderr, "note: -run latest is run %s, the newest record, a `shrt verify` replay, as shrt diff picks it; to slice from the newest `shrt run` record: -run %s\n",
			latest.RunID, own.RunID)
		return latest, nil
	}
	fmt.Fprintf(os.Stderr, "note: -run latest is run %s, the newest `shrt run` record of %s; the newest record, %s, is a `shrt verify` replay recorded right after it: pass -run %s to slice from it\n",
		own.RunID, chainName, latest.RunID, latest.RunID)
	return own, nil
}

func stepStatus(rec *runner.Record, step string) string {
	if sr, ok := rec.Step(step); ok {
		return sr.Status
	}
	return "was not run"
}

func stepFailed(rec *runner.Record, step string) bool {
	sr, ok := rec.Step(step)
	return ok && sr.Status == runner.StatusFailed
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
	msg := fmt.Sprintf("run %s did not reach step %q (%s), so it has no value to pin and no verdict to compare", rec.RunID, step, why)
	other, err := newestRunReaching(e, chainName, step, rec.RunID)
	if err != nil {
		return nil, err
	}
	if other == nil {
		return nil, fmt.Errorf("%s, and no recorded run of %s reached it.\nRun the chain until it reaches the step: shrt run %s", msg, chainName, ref)
	}
	return nil, fmt.Errorf("%s.\nRun %s did: pass -run %s", msg, other.RunID, other.RunID)
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
	relaxable := relaxableIn(rec)
	for _, id := range ids {
		sr, ok := rec.Step(id)
		if ok && (sr.Status == runner.StatusFailed || sr.Status == runner.StatusError) && !relaxable(id) {
			blocked = append(blocked, id+" ("+sr.Status+")")
			continue
		}
		usable = append(usable, id)
	}
	return usable, blocked
}

func pinnedWrites(res *chain.SliceResult) []string {
	if res.Mode != chain.SliceModePin || res.Chain == nil || len(res.Pins) == 0 {
		return nil
	}
	out := []string{}
	for _, st := range res.Chain.Steps {
		if chain.IsReadOnlyCall(st.Call) {
			continue
		}
		raw, err := json.Marshal(map[string]any{"body": st.Body, "headers": st.Headers})
		if err != nil {
			continue
		}
		used := []string{}
		for _, p := range res.Pins {
			if strings.Contains(string(raw), "${vars."+p.Var+"}") {
				used = append(used, fmt.Sprintf("%s=%v", p.Ref, p.Value))
			}
		}
		if len(used) > 0 {
			out = append(out, fmt.Sprintf("%s (%s) on %s", st.ID, shortCall(st.Call), strings.Join(used, ", ")))
		}
	}
	return out
}

func closureCommand(c *chain.Chain, step string, opts chain.SliceOptions, res *chain.SliceResult, vars varFlags) string {
	parts := []string{"shrt chain slice", res.SourceCommandRef(), "-step", res.Target, "-keep", chain.SliceKeepWrites}
	if res.Run != "" {
		parts = append(parts, "-run", res.Run)
	}
	o := opts
	o.Mode, o.Keep = chain.SliceModeClosure, []string{chain.SliceKeepWrites}
	names := map[string]bool{}
	if next, err := chain.Slice(c, step, o); err == nil {
		for _, n := range next.FreshVars {
			names[n] = true
		}
	}
	for k := range vars {
		names[k] = true
	}
	sorted := make([]string, 0, len(names))
	for k := range names {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		value := "<fresh>"
		if v, given := vars[k]; given {
			value = fmt.Sprint(v)
		}
		parts = append(parts, "-var", k+"="+value)
	}
	return strings.Join(parts, " ") + " -verify"
}

func pinnedWritesError(res *chain.SliceResult, pinned []string, closure string) error {
	return fmt.Errorf("refusing to -verify: under -mode pin the slice re-sends write step(s) on what run %s created: %s.\n"+
		"That run already did those writes, so sending them changes live entities a second time "+
		"and the verdict is that of a second write, not the one recorded.\n"+
		"Reproduce it in closure mode, which creates what the write needs afresh: %s\n"+
		"or pass -resend-writes to send it anyway", res.Run, strings.Join(pinned, "; "), closure)
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

func sameSliceFile(a, b string) bool {
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
