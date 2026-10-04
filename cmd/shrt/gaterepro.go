package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/runner"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type gateRef struct {
	chain string
	it    gateItem
}

func gateRepro(ctx context.Context, e *env, chains []*gateChain, groups []*gateGroup, wait time.Duration) {
	spot := map[string]bool{}
	for _, g := range chains {
		spot[g.name] = g.spot
	}
	fresh := func(*chain.Chain, string) []string { return nil }
	if lib, err := e.library(); err == nil {
		fresh = freshVarsOf(e, lib)
	}
	if len(groups) > 0 {
		fmt.Fprintf(os.Stderr, "gate: -repro: writing and verifying a repro for each of %d suspect rpc(s), each slice sent %d times\n", len(groups), sliceRepeat)
		fmt.Println("repro, for each row of failures by suspect rpc:")
	}
	done := map[string]string{}
	for _, gr := range groups {
		fmt.Println("  " + gr.head)
		for _, line := range settleRow(ctx, e, gr, spot, wait) {
			fmt.Println("    " + line)
		}
		at := gateRef{chain: gr.in, it: gr.example}
		key := at.chain + " " + at.it.Step
		if done[key] == "" {
			done[key] = reproRow(ctx, e, at, fresh)
		}
		fmt.Println("    " + done[key])
	}
	fmt.Println(gateMasks(ctx, chains))
}

func scratchDir(e *env) string {
	return e.cfg.Abs(filepath.Join(config.DirName, "scratch"))
}

func settleRow(ctx context.Context, e *env, gr *gateGroup, spot map[string]bool, wait time.Duration) []string {
	var out []string
	asked := map[string]bool{}
	refs := append([]gateRef{{chain: gr.in, it: gr.example}}, gr.refs...)
	for _, onSpot := range []bool{true, false} {
		for _, ref := range refs {
			r := ref.it.Reason
			pair := r.RPC + " " + r.ReadRPC + " " + leafOf(ref.it.Path)
			if spot[ref.chain] != onSpot || r.Read != ref.it.Step || asked[pair] {
				continue
			}
			if _, via, _ := tellApartReads(e, r, ref.it.Path); len(via) > 0 {
				asked[pair] = true
				if line := settle(ctx, e, ref, via[0], wait); line != "" {
					out = append(out, line)
				}
			}
		}
	}
	return out
}

func settle(ctx context.Context, e *env, ref gateRef, via tellRead, wait time.Duration) string {
	r, path := ref.it.Reason, ref.it.Path
	c, err := e.resolveChain(ref.chain)
	if err != nil {
		return "not settled: " + err.Error()
	}
	at := slices.IndexFunc(c.Steps, func(s *chain.Step) bool { return s.ID == r.Read })
	w := slices.IndexFunc(c.Steps, func(s *chain.Step) bool { return s.ID == r.Step })
	rm, err := e.cat.Lookup(r.ReadRPC)
	if at < 0 || w < 0 || w > at || err != nil {
		return ""
	}
	steps := slices.Clone(c.Steps[:at+1])
	expect := func(i int, p string, want any, replace bool) {
		same := func(x chain.Expectation) bool { return x.Path == p }
		if !replace && slices.ContainsFunc(steps[i].Expect, same) {
			return
		}
		st := *steps[i]
		st.Expect = append(slices.DeleteFunc(slices.Clone(st.Expect), same), chain.Expectation{Path: p, Equals: want})
		steps[i] = &st
	}
	expect(w, r.Path, r.Want, false)
	expect(at, path, "${"+r.Step+"."+r.Path+"}", true)
	read := steps[at]
	field, viaName := gateIndex.ReplaceAllString(path, "[]$1"), shortRPC(via.method.FullName)
	tc := &chain.Chain{APIVersion: c.APIVersion, Name: c.Name + "-tell-apart-" + r.Read, Vars: c.Vars, Volatile: c.Volatile, Unordered: c.Unordered, Redact: c.Redact,
		Description: fmt.Sprintf("Written by shrt gate -repro: %s up to %s, then %s reads %s of the same item, to tell the write %s from the read %s.", c.Name, r.Read, viaName, field, r.Step, r.Read),
		Steps:       append(steps, &chain.Step{ID: r.Read + "_tell_apart", Call: via.method.FullName, Auth: read.Auth, SkipAuth: read.SkipAuth, Body: tellApartBody(via.method, rm, read, path)})}
	file := filepath.Join(scratchDir(e), tc.Name+".yaml")
	if err := writeSliceFile(file, tc); err != nil {
		return "not settled: " + err.Error()
	}
	out := gateAttempt(ctx, "run", file, len(tc.UnusedVarNames(map[string]any{chain.RunTagVar: ""})) == 0, wait)
	shown := "shrt run " + shownPath(file)
	for _, x := range out.side.Items {
		if x.Step != r.Read || x.Path != path {
			continue
		}
		switch x.Reason.Kind {
		case reasonStored:
			return fmt.Sprintf("settled on the write %s (%s): %s read %s=%s, as %s did, where the write answered %s  (%s)",
				x.Reason.Step, shortRPC(x.Reason.RPC), viaName, field, valueText(x.Got), methodName(r.ReadRPC), valueText(x.Want), shown)
		case reasonDiffers:
			return fmt.Sprintf("settled on the read %s (%s): %s read %s=%s, as the write %s answered, where %s read %s  (%s)",
				r.Read, shortRPC(r.ReadRPC), viaName, field, valueText(x.Want), r.Step, methodName(r.ReadRPC), valueText(x.Got), shown)
		}
		return fmt.Sprintf("not settled: %s did not read %s of the same item  (%s)", viaName, field, shown)
	}
	if out.code != 0 && out.code != 1 {
		return fmt.Sprintf("not settled: %s  (%s)", lastLine(out), shown)
	}
	return fmt.Sprintf("not settled: %s read %s as %s answered it in the tell-apart run  (%s)", r.Read, field, r.Step, shown)
}

func tellApartBody(m, read *catalog.Method, st *chain.Step, path string) map[string]any {
	segs := chain.SplitPath(path)
	at := strings.Join(segs[:len(segs)-1], ".")
	carrier := read.Output()
	for _, seg := range segs[:len(segs)-1] {
		if fd := carrier.Fields().ByName(protoreflect.Name(seg)); fd != nil && fd.Message() != nil {
			carrier = fd.Message()
		}
	}
	ref := func(name string) any {
		if carrier.Fields().ByName(protoreflect.Name(name)) == nil {
			return nil
		}
		return "${" + strings.TrimPrefix(st.ID+"."+at+"."+name, ".") + "}"
	}
	body := map[string]any{}
	fields := m.Input().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		name := string(fd.Name())
		if fd.IsList() || fd.IsMap() || fd.Message() != nil {
			continue
		}
		if key, ok := namecase.LookupKey(st.Body, name); ok {
			body[name] = "${steps." + st.ID + ".request." + key + "}"
		} else if v := ref(name); v != nil {
			body[name] = v
		} else if lower := strings.ToLower(name); strings.Contains(lower, "prefix") {
			if v := ref(strings.Trim(strings.ReplaceAll(lower, "prefix", ""), "_")); v != nil {
				body[name] = v
			}
		}
	}
	return body
}

func reproRow(ctx context.Context, e *env, ref gateRef, fresh func(*chain.Chain, string) []string) string {
	step := ref.it.Step
	file := filepath.Join(scratchDir(e), strings.TrimSuffix(filepath.Base(ref.chain), ".yaml")+"-slice-"+step+".yaml")
	args := []string{"chain", "slice", ref.chain, "-step", step, "-run", "latest", "-verify", "-minimize", "-write=" + file, "-json"}
	var vars []string
	if c, err := e.resolveChain(ref.chain); err == nil {
		vars = fresh(c, step)
		for _, v := range vars {
			args = append(args, "-var", v+"="+chain.NewRunTag())
		}
		at, keep := slices.IndexFunc(c.Steps, func(s *chain.Step) bool { return s.ID == step }), []string{}
		for _, r := range append([]reason{{Step: ref.it.suspect()}}, ref.it.Reason.Or...) {
			if j := slices.IndexFunc(c.Steps, func(s *chain.Step) bool { return s.ID == r.Step }); j >= 0 && j < at && !slices.Contains(keep, r.Step) {
				keep = append(keep, r.Step)
			}
		}
		if len(keep) > 0 {
			args = append(args, "-keep", strings.Join(keep, ","))
		}
	}
	s, why := sliceRepro(ctx, args)
	if s != nil && s.Verify.Outcome != sliceReproduced {
		if w, _ := sliceRepro(ctx, append(args, "-minimize=false")); w != nil {
			s = w
		}
	}
	if next := strings.Fields(nextOf(s)); len(next) > 1 && next[0] == "shrt" {
		if w, _ := sliceRepro(ctx, append(next[1:], "-minimize", "-json")); w != nil {
			s = w
		}
	}
	if s == nil {
		return "repro: none: " + why
	}
	if v := s.Verify; v.Outcome == sliceReproduced || v.Outcome == sliceIntermittent {
		file = cmp.Or(s.Written, file)
		flag, note := readBack(ctx, e, ref, file, vars)
		kept := len(s.Kept)
		if flag != "" {
			kept++
		}
		return fmt.Sprintf("repro: shrt run %s%s  (%d of %d steps, %s%s%s)", shownPath(file), flag, kept, s.Total, outcomeWord(v.Outcome), v.countLabel(), note)
	}
	v := s.Verify
	if why = outcomeWord(v.Outcome); v.err() != nil {
		why = v.err().Error()
	}
	if first, _, _ := strings.Cut(v.Reason, "\n"); first != "" {
		why += ": " + first
	} else if len(v.Differences) > 0 {
		why += ": " + v.Differences[0]
	}
	return fmt.Sprintf("repro: none: %s (slice of %s)", why, ref.chain)
}

func readBack(ctx context.Context, e *env, ref gateRef, file string, vars []string) (string, string) {
	r, path := ref.it.Reason, ""
	if r.Kind != reasonStored || r.Read == "" {
		return "", ""
	}
	c, err := e.resolveChain(ref.chain)
	slice, sErr := chain.LoadFile(file)
	rec, rErr := e.store.LatestRun(ref.chain)
	if err != nil || sErr != nil || rErr != nil {
		return "", ""
	}
	read, ok := c.Step(r.Read)
	if st, found := rec.Step(r.Read); ok && found && st != nil {
		eachLeaf(decoded(st.Response), "", func(p string, v any) {
			if path == "" && leafOf(p) == leafOf(r.Path) && compactValue(v) == r.Got {
				path = p
			}
		})
	}
	if _, kept := slice.Step(r.Read); path == "" || kept {
		return "", ""
	}
	field := gateIndex.ReplaceAllString(path, "[]$1")
	orig, _ := os.ReadFile(file)
	back := *read
	back.Expect = []chain.Expectation{{Path: path, Equals: r.Want}}
	slice.Steps = append(slice.Steps, &back)
	slice.Description = strings.TrimSpace(slice.Description) + fmt.Sprintf("\nThen %s reads %s back and expects what %s answered, %s: run it with -keep-going to see the answer and the stored value side by side.", r.Read, field, r.Step, valueText(r.Want))
	args := []string{"run", file, "-quiet", "-keep-going"}
	for _, v := range vars {
		args = append(args, "-var", v+"="+chain.NewRunTag())
	}
	if writeSliceFile(file, slice) == nil {
		items := gateExec(ctx, args).side.Items
		if slices.ContainsFunc(items, func(x gateItem) bool { return x.Step == r.Step }) && slices.ContainsFunc(items, func(x gateItem) bool { return x.Step == r.Read && x.Path == path }) {
			return " -keep-going", fmt.Sprintf("; the read-back %s (%s) reads %s=%s where the write answered %s", r.Read, methodName(read.Call), field, valueText(r.Got), valueText(r.Want))
		}
	}
	_ = os.WriteFile(file, orig, 0o644)
	return "", ""
}

type sliceOut struct {
	Written string        `json:"written"`
	Kept    []chain.Keep  `json:"kept"`
	Total   int           `json:"total"`
	Verify  *sliceVerdict `json:"verify"`
}

func nextOf(s *sliceOut) string {
	if s == nil || s.Verify.Outcome == sliceReproduced {
		return ""
	}
	return strings.ReplaceAll(s.Verify.Next, "<fresh>", chain.NewRunTag())
}

func sliceRepro(ctx context.Context, args []string) (*sliceOut, string) {
	out, res := gateExec(ctx, args), &sliceOut{}
	if json.Unmarshal([]byte(out.stdout), res) != nil || res.Verify == nil {
		return nil, lastLine(out)
	}
	return res, ""
}

func lastLine(out gateOutcome) string {
	lines := strings.Split(strings.TrimSpace(out.stderr+"\n"+out.side.Error), "\n")
	return strings.TrimPrefix(strings.TrimPrefix(lines[len(lines)-1], "shrt chain: "), "shrt run: ")
}

func gateMasks(ctx context.Context, chains []*gateChain) string {
	read, masked, inLists, beyond, lists, unread := 0, 0, 0, []string{}, []string{}, []string{}
	for _, g := range chains {
		if !g.spot || g.skipped {
			continue
		}
		var out struct {
			Run struct {
				Vars map[string]any `json:"vars"`
			} `json:"run"`
			Diff *diff.Report `json:"diff"`
		}
		if json.Unmarshal([]byte(gateExec(ctx, []string{"verify", g.name, "-run", "latest", "-json"}).stdout), &out) != nil || out.Diff == nil {
			unread = append(unread, g.name)
			continue
		}
		r := out.Diff
		tag, _ := out.Run.Vars[chain.RunTagVar].(string)
		read++
		masked += len(r.VolatileValues) + len(r.ShapeMasked) + len(r.FixtureEchoed) + len(r.FixtureInput)
		for _, c := range r.VolatileValues {
			switch {
			case wholeList(c):
				inLists++
				if at := g.name + " " + c.Step; !slices.Contains(lists, at) {
					lists = append(lists, at)
				}
			case c.Kind != diff.KindChanged || !diff.LooksVolatile(c.Path, c.Want, c.Got) && !tagOnly(c, tag):
				beyond = append(beyond, fmt.Sprintf("%s %s %s (%s)", g.name, c.Step, c.Path, c.Transition()))
			}
		}
	}
	more := ""
	if inLists > 0 {
		more = fmt.Sprintf(", leaving out %d inside whole lists a step marks volatile, which hold whatever else the backend holds (%s)", inLists, capList(lists, 3))
	}
	if len(unread) > 0 {
		more += "; no record to read for " + capList(unread, 5)
	}
	switch {
	case read == 0 && len(unread) == 0:
		return "masks: no chain here has a safe spot, so verify masked nothing"
	case len(beyond) == 0:
		return fmt.Sprintf("masks: none of the %d masked or volatile value(s) in %d chain(s) differed beyond run tags, ids and timestamps%s; "+
			"shrt verify <chain> -run latest -masked lists them", masked, read, more)
	}
	shown := beyond[:min(len(beyond), 10)]
	if n := len(beyond) - len(shown); n > 0 {
		shown = append(shown, fmt.Sprintf("and %d more: shrt verify <chain> -run latest -masked", n))
	}
	return fmt.Sprintf("masks: %d masked or volatile value(s) differed beyond run tags, ids and timestamps%s:\n  %s", len(beyond), more, strings.Join(shown, "\n  "))
}

func wholeList(c diff.Change) bool {
	rest, under := strings.CutPrefix(c.Path, c.Mask+".")
	if c.Mask == "" || strings.Contains(c.Mask, "*") {
		return false
	}
	return c.Path == c.Mask && c.Kind == diff.KindLength || under && rest != "" && rest[0] >= '0' && rest[0] <= '9'
}

func tagOnly(c diff.Change, tag string) bool {
	want, okW := c.Want.(string)
	got, okG := c.Got.(string)
	i := strings.Index(got, tag)
	if !okW || !okG || len(tag) < 4 || i < 0 {
		return false
	}
	pre, post := got[:i], got[i+len(tag):]
	return len(want) > len(pre)+len(post) && strings.HasPrefix(want, pre) && strings.HasSuffix(want, post)
}

var gapProbes = 4

type gapTally struct{ probed, failed, left int }

func gateGaps(ctx context.Context, e *env, gated []*gateChain, wait time.Duration) (string, gapTally) {
	lib, err := e.library()
	if err != nil || e.cat == nil || len(lib.Overlays) == 0 {
		return "", gapTally{}
	}
	chains, _, _ := chain.LoadDirPartial(e.chainsDir())
	sent := []*chain.Chain{}
	for _, c := range chains {
		if slices.ContainsFunc(gated, func(g *gateChain) bool { return g.name == c.Name && !g.skipped }) {
			sent = append(sent, c)
		}
	}
	covered := calledRPCs(e, sent)
	_, plans := reachableRPCs(lib, e.cat)
	gaps := slices.DeleteFunc(contract.StateGaps(chains, plans, lib, e.cat), func(g contract.StateGap) bool { return !covered[g.RPC] })
	if len(gaps) == 0 {
		return "gaps: none: each write this gate calls is called from every state its plan calls it from, with each item count", gapTally{}
	}
	began, runs, lines, tally := time.Now(), map[string]*gapRun{}, []string{}, gapTally{left: max(len(gaps)-8, 0)}
	for i, g := range gaps[:min(len(gaps), 8)] {
		lines = append(lines, "  "+g.Line())
		if i >= gapProbes {
			lines[len(lines)-1] += ": not probed: " + gapPlan(g.RPC)
			tally.left++
			continue
		}
		if runs[g.RPC] == nil {
			fmt.Fprintf(os.Stderr, "gate: -repro: planning %s into .shrt/scratch/ and running it once, for the state(s) no chain calls it from\n", methodName(g.RPC))
			runs[g.RPC] = runGap(ctx, e, lib, g.RPC, wait)
		}
		probe := runs[g.RPC].probe(ctx, e, lib, g)
		for _, line := range probe {
			lines = append(lines, "    "+line)
		}
		if strings.HasPrefix(probe[0], "not probed") {
			tally.left++
			continue
		}
		tally.probed++
		if !strings.HasPrefix(probe[0], "passes") {
			tally.failed++
		}
	}
	if len(gaps) > 8 {
		lines = append(lines, fmt.Sprintf("  and %d more: shrt contract status -gaps", len(gaps)-8))
	}
	return fmt.Sprintf("gaps: %d state(s) no chain calls a gated write from, so no row above can show a fault there; -repro planned and ran %d of them "+
		"in .shrt/scratch/ (%s). No safe spot covers these states, so a failure here is no regression, only a miss against what the contract and its plan expect:\n%s",
		len(gaps), min(len(gaps), gapProbes), time.Since(began).Round(time.Second), strings.Join(lines, "\n")), tally
}

func gapFile(rpc string) string {
	return strings.ToLower(strings.ReplaceAll(shortRPC(rpc), "/", "-")) + "-gaps.yaml"
}

func gapPlan(rpc string) string {
	return fmt.Sprintf("shrt contract plan %s -write %s (into .shrt/scratch/)", methodName(rpc), gapFile(rpc))
}

type gapRun struct {
	file, why string
	c         *chain.Chain
	side      gateSidecar
	rec       *runner.Record
}

func runGap(ctx context.Context, e *env, lib *contract.Library, rpc string, wait time.Duration) *gapRun {
	r := &gapRun{file: filepath.Join(scratchDir(e), gapFile(rpc))}
	name, began := strings.TrimSuffix(gapFile(rpc), ".yaml"), time.Now()
	plan, err := contract.BuildPlanWith([]string{rpc}, lib, e.cat, name, planOptions(e))
	var raw []byte
	if err == nil && plan.UnfilledCount() > 0 {
		err = fmt.Errorf("its plan leaves %d required field(s) without test data: shrt contract plan %s -notes", plan.UnfilledCount(), methodName(rpc))
	}
	if err == nil {
		raw, err = plan.YAML()
	}
	if err == nil {
		err = writePlanFile(r.file, raw)
	}
	if err != nil {
		r.why = err.Error() + ": " + gapPlan(rpc)
		return r
	}
	r.c = plan.Chain
	out := gateAttempt(ctx, "run", r.file, len(r.c.UnusedVarNames(map[string]any{chain.RunTagVar: ""})) == 0, wait)
	r.side, r.why = out.side, lastLine(out)+"  (shrt run "+shownPath(r.file)+")"
	if ids, err := e.store.ListRuns(name); err == nil && len(ids) > 0 && out.code != 3 {
		if rec, err := e.store.LoadRun(name, ids[len(ids)-1]); err == nil && !rec.StartedAt.Before(began.Truncate(time.Second)) {
			r.rec = rec
		}
	}
	return r
}

func (r *gapRun) probe(ctx context.Context, e *env, lib *contract.Library, g contract.StateGap) []string {
	if r.rec == nil {
		return []string{"not probed: " + r.why}
	}
	shown, behind := shownPath(r.file), ""
	sits, calls := contract.Situations(r.c, lib, e.cat), []string{}
	for _, st := range r.c.Steps {
		if m, err := e.cat.Lookup(st.Call); err == nil && m.FullName == g.RPC && sits[st.ID].State == g.State {
			calls = append(calls, st.ID)
		}
	}
	failing, byRPC, order := map[string]bool{}, map[string][]gateItem{}, []string{}
	for _, it := range r.side.Items {
		at := cmp.Or(it.suspect(), it.Step)
		switch {
		case it.Passes:
		case slices.Contains(calls, at):
			failing[at] = true
			if byRPC[it.rpc()] == nil {
				order = append(order, it.rpc())
			}
			byRPC[it.rpc()] = append(byRPC[it.rpc()], it)
		case slices.Contains(calls, it.Step) && behind == "":
			behind = fmt.Sprintf("not probed: %s %s; %s  (shrt run %s)", it.Step, it.headline(), it.Reason.in(said{row: true}), shown)
		}
	}
	var fails, passes []rowCall
	for _, id := range calls {
		st, found := r.rec.Step(id)
		if !found || st == nil {
			continue
		}
		dims := requestDims(st)
		if sit := sits[id]; sit.List != "" && dims["len "+sit.List] == "" {
			dims["len "+sit.List] = fmt.Sprint(sit.Items)
		}
		switch {
		case failing[id]:
			fails = append(fails, rowCall{at: r.c.Name + " " + id, dims: dims})
		case st.Status == runner.StatusPassed:
			passes = append(passes, rowCall{at: r.c.Name + " " + id, dims: dims})
		}
	}
	if len(order) == 0 {
		if behind != "" {
			return []string{behind}
		}
		if len(calls) == 0 || len(passes) < len(calls) {
			return []string{fmt.Sprintf("not probed: %d of its plan's %d call(s) from that state passed, and none failed  (shrt run %s)", len(passes), len(calls), shown)}
		}
		return []string{fmt.Sprintf("passes: %d call(s) from that state, %s  (shrt run %s)", len(calls), capList(calls, 3), shown)}
	}
	trigger, _ := triggerOf(fails, passes)
	var out []string
	for _, rpc := range order {
		its, fields, steps := byRPC[rpc], []string{}, map[string]bool{}
		ex := its[0]
		for _, it := range its {
			if f, _ := it.field(); f != "" && !slices.Contains(fields, f) {
				fields = append(fields, f)
			}
			steps[it.Step] = true
			if it.suspect() == "" && ex.suspect() != "" {
				ex = it
			}
		}
		line := fmt.Sprintf("%s: %d step(s) in 1 chain(s); e.g. %s %s", strings.TrimSpace(rpc+" "+capList(fields, 3)), len(steps), r.c.Name, ex.Step)
		if why := ex.Reason.in(said{step: ex.Step, rpc: rpc}); why != "" {
			line += "; " + why
		} else {
			path, eg := ex.shown()
			line += " " + path + " " + eg
			if st, found := r.rec.Step(ex.Step); found && st != nil && refusalOf(st) != "" && refusalOf(st) != ex.Got {
				line += " (" + refusalOf(st) + ")"
			}
		}
		out = append(out, line)
		if trigger != "" && rpc == shortRPC(g.RPC) {
			out = append(out, trigger)
		}
		out = append(out, r.repro(ctx, e, lib, g.RPC, calls, ex))
	}
	return out
}

func (r *gapRun) repro(ctx context.Context, e *env, lib *contract.Library, rpc string, own []string, ex gateItem) string {
	refs := func(st *chain.Step) []string {
		var out []string
		for _, ref := range st.SendReferences() {
			if id, ok := chain.ParseRef(ref).StepID(); ok && slices.ContainsFunc(r.c.Steps, func(s *chain.Step) bool { return s.ID == id }) {
				out = append(out, id)
			}
		}
		return out
	}
	need, drop := map[string]bool{}, []string{}
	for queue := slices.Clone(own); len(queue) > 0; queue = queue[1:] {
		if st, found := r.c.Step(queue[0]); found && !need[st.ID] {
			need[st.ID], queue = true, append(queue, refs(st)...)
		}
	}
	for _, st := range r.c.Steps {
		if m, err := e.cat.Lookup(st.Call); err == nil && m.FullName == rpc && !need[st.ID] {
			by := slices.DeleteFunc(refs(st), func(id string) bool { return need[id] })
			if len(by) == 0 {
				by = []string{st.ID}
			}
			drop = append(drop, by...)
		}
	}
	fresh, focus := freshVarsOf(e, lib), filepath.Join(scratchDir(e), "focus", filepath.Base(r.file))
	if res, err := chain.Without(r.c, drop, r.c.Name); err == nil && writeSliceFile(focus, res.Chain) == nil {
		defer os.RemoveAll(filepath.Dir(focus))
		if line := reproRow(ctx, e, gateRef{chain: focus, it: ex}, fresh); !strings.HasPrefix(line, "repro: none") {
			return line
		}
	}
	return reproRow(ctx, e, gateRef{chain: r.file, it: ex}, fresh)
}
