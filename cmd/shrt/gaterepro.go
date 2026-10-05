package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
	spot, byName := map[string]bool{}, map[string]*gateChain{}
	for _, g := range chains {
		spot[g.name], byName[g.name] = g.spot, g
	}
	fresh := func(*chain.Chain, string) []string { return nil }
	if lib, err := e.library(); err == nil {
		fresh = freshVarsOf(e, lib)
	}
	if len(groups) > 0 {
		fmt.Fprintf(os.Stderr, "gate: -repro: writing and verifying a repro for each of %d suspect rpc(s), each slice sent %d times\n", len(groups), sliceRepeat)
		fmt.Println("repro, for each row of failures by suspect rpc:")
	}
	done, files := map[string]string{}, map[string]string{}
	for _, gr := range groups {
		fmt.Println("  " + gr.head)
		for _, line := range settleRow(ctx, e, gr, spot, wait) {
			fmt.Println("    " + line)
		}
		at := gateRef{chain: gr.in, it: gr.example}
		key := at.chain + " " + at.it.Step
		if done[key] == "" {
			done[key], files[key] = reproRow(ctx, e, at, fresh)
		}
		fails, passes := gr.rowCalls(byName)
		switch line, against := firmTrigger(ctx, e, files[key], at, fails, passes); {
		case against != "":
			fmt.Println("    trigger above does not hold: " + against)
		case line != gr.trigger && gr.trigger != "":
			fmt.Println("    trigger, firmed by -repro in place of the row's line above: " + strings.TrimPrefix(line, "trigger: "))
		case line != gr.trigger:
			fmt.Println("    " + line)
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
			r, key := ref.it.Reason, ""
			_, via, _ := tellApartReads(e, r, ref.it.Path)
			switch {
			case spot[ref.chain] != onSpot:
			case r.Kind == reasonUnclear && len(r.Or) == 2:
				key = ref.chain + " " + r.Or[0].Step + " " + r.Or[1].Step
			case r.Read == ref.it.Step && len(via) > 0:
				key = r.RPC + " " + r.ReadRPC + " " + leafOf(ref.it.Path)
			}
			if key == "" || asked[key] {
				continue
			}
			asked[key] = true
			line := ""
			if len(via) > 0 {
				line = settle(ctx, e, ref, via[0], wait)
			} else {
				line = settleWrites(ctx, e, ref, wait)
			}
			if line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

func tellApartRun(ctx context.Context, e *env, c *chain.Chain, read, about string, steps []*chain.Step, wait time.Duration) (gateOutcome, string, error) {
	tc := &chain.Chain{APIVersion: c.APIVersion, Name: c.Name + "-tell-apart-" + read, Vars: c.Vars, Volatile: c.Volatile, Unordered: c.Unordered, Redact: c.Redact,
		Description: "Written by shrt gate -repro: " + c.Name + " up to " + read + about, Steps: steps}
	file := filepath.Join(scratchDir(e), tc.Name+".yaml")
	if err := writeSliceFile(file, tc); err != nil {
		return gateOutcome{}, "", err
	}
	return gateAttempt(ctx, "run", file, len(tc.UnusedVarNames(map[string]any{chain.RunTagVar: ""})) == 0, wait), "shrt run " + shownPath(file), nil
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
	out, shown, err := tellApartRun(ctx, e, c, r.Read, fmt.Sprintf(", then %s reads %s of the same item, to tell the write %s from the read %s.", viaName, field, r.Step, r.Read),
		append(steps, &chain.Step{ID: r.Read + "_tell_apart", Call: via.method.FullName, Auth: read.Auth, SkipAuth: read.SkipAuth, Body: tellApartBody(via.method, rm, read, path)}), wait)
	if err != nil {
		return "not settled: " + err.Error()
	}
	for _, x := range out.side.Items {
		if x.Step != r.Read || x.Path != path {
			continue
		}
		switch x.Reason.Kind {
		case reasonStored:
			return fmt.Sprintf("settled on the write %s (%s): %s read %s=%s, as %s did, where the write answered %s  (%s)",
				x.Reason.Step, shortRPC(x.Reason.RPC), viaName, field, valueText(x.Got), chain.RPCName(r.ReadRPC), valueText(x.Want), shown)
		case reasonDiffers:
			return fmt.Sprintf("settled on the read %s (%s): %s read %s=%s, as the write %s answered, where %s read %s  (%s)",
				r.Read, shortRPC(r.ReadRPC), viaName, field, valueText(x.Want), r.Step, chain.RPCName(r.ReadRPC), valueText(x.Got), shown)
		}
		return fmt.Sprintf("not settled: %s did not read %s of the same item  (%s)", viaName, field, shown)
	}
	if out.code != 0 && out.code != 1 {
		return fmt.Sprintf("not settled: %s  (%s)", lastLine(out), shown)
	}
	return fmt.Sprintf("not settled: %s read %s as %s answered it in the tell-apart run  (%s)", r.Read, field, r.Step, shown)
}

func settleWrites(ctx context.Context, e *env, ref gateRef, wait time.Duration) string {
	path, early, late, step := ref.it.Path, ref.it.Reason.Or[0], ref.it.Reason.Or[1], ref.it.Step
	leaf, field := leafOf(path), gateIndex.ReplaceAllString(path, "[]$1")
	rec, err := e.store.LatestRun(ref.chain)
	if eff := e.effectsOf(late.RPC)[leaf]; eff == nil || cmp.Or(eff.Increase, eff.Decrease) == "" || err != nil {
		return ""
	}
	at := func(id string) *runner.StepRecord {
		st, _ := rec.Step(id)
		return st
	}
	_, ids, why := valueAt(at(step), path)
	pos, read, shown := positions(rec), "", ""
	from, found := pos[early.Step]
	for i := from + 1; found && why == "" && i < pos[late.Step] && read == ""; i++ {
		if _, found, _ := entityValue(rec.Steps[i], leaf, ids); found && !isWrite(rec.Steps[i]) {
			read = rec.Steps[i].ID
		}
	}
	if read == "" {
		c, err := e.resolveChain(ref.chain)
		if err != nil {
			return ""
		}
		w := slices.IndexFunc(c.Steps, func(s *chain.Step) bool { return s.ID == early.Step })
		r := slices.IndexFunc(c.Steps, func(s *chain.Step) bool { return s.ID == step })
		if w < 0 || r < w || slices.ContainsFunc(c.Steps[r].SendReferences(), func(x string) bool {
			id, ok := chain.ParseRef(x).StepID()
			return ok && slices.ContainsFunc(c.Steps[w+1:r+1], func(s *chain.Step) bool { return s.ID == id })
		}) {
			return ""
		}
		again := *c.Steps[r]
		again.ID, again.Expect, again.Export = step+"_after_"+early.Step, nil, nil
		out, file, err := tellApartRun(ctx, e, c, step, fmt.Sprintf(", with %s sent again right after %s, to tell the write %s from the write %s.", step, early.Step, early.Step, late.Step),
			slices.Concat(c.Steps[:w+1], []*chain.Step{&again}, c.Steps[w+1:r+1]), wait)
		if err != nil {
			return "not settled: " + err.Error()
		}
		if shown, read = "  ("+file+")", again.ID; out.code != 0 && out.code != 1 {
			return "not settled: " + lastLine(out) + shown
		}
		if rec, err = e.store.LatestRun(c.Name + "-tell-apart-" + step); err != nil {
			return "not settled: " + err.Error() + shown
		}
	}
	now, ids, why := valueAt(at(step), path)
	w, okW, _ := entityValue(at(early.Step), leaf, ids)
	v, okV, _ := entityValue(at(read), leaf, ids)
	want, okWant := number(ref.it.Want)
	sw, against := w, "the chain's expected values"
	if spot, err := e.store.LoadSafeSpot(ref.chain); err == nil {
		approved := &runner.Record{Steps: spot.Steps}
		sAt, _ := approved.Step(step)
		sEarly, _ := approved.Step(early.Step)
		sNow, sIDs, sWhy := valueAt(sAt, path)
		sw, okWant, _ = entityValue(sEarly, leaf, sIDs)
		want, against, okWant = sNow, "the approved run", okWant && sWhy == ""
	}
	if why != "" || !okW || !okV || !okWant {
		return fmt.Sprintf("not settled: the runs show no %s of that record to compare%s", field, shown)
	}
	by := shortRPC(at(read).Call)
	if shown == "" {
		by = read + " (" + by + ")"
	}
	stored, as := v != w, now-v == want-sw
	verdict, text, sep := fmt.Sprintf("not settled: %s or %s in %s", early.Step, late.Step, ref.chain), fmt.Sprintf("%s read %s=%s right after %s", by, field, compactValue(v), early.Step), " where "
	switch {
	case stored && as:
		verdict, text = fmt.Sprintf("settled on the write %s (%s) in %s", early.Step, shortRPC(early.RPC), ref.chain), fmt.Sprintf("%s read %s=%s right after it where the write answered %s", by, field, compactValue(v), compactValue(w))
	case stored:
		text += " where it answered " + compactValue(w)
	case !as && w == sw:
		verdict = fmt.Sprintf("settled on the write %s (%s) in %s", late.Step, shortRPC(late.RPC), ref.chain)
		fallthrough
	default:
		text += ", as it answered"
	}
	if as {
		sep = ", as "
	}
	return fmt.Sprintf("%s: %s, and %s %s%s%s %s%s", verdict, text, late.Step, moved(v, now), sep, against, moved(sw, want), shown)
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

func reproRow(ctx context.Context, e *env, ref gateRef, fresh func(*chain.Chain, string) []string) (string, string) {
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
		for _, r := range append([]reason{{Step: ref.it.suspect()}, {Step: clearingRead(e, ref)}}, ref.it.Reason.Or...) {
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
		return "repro: none: " + why, ""
	}
	if v := s.Verify; v.Outcome == sliceReproduced || v.Outcome == sliceIntermittent {
		file = cmp.Or(s.Written, file)
		flag, note := readBack(ctx, e, ref, file, vars)
		kept := len(s.Kept)
		if flag != "" {
			kept++
		}
		return fmt.Sprintf("repro: shrt run %s%s  (%d of %d steps, %s%s%s)", shownPath(file), flag, kept, s.Total, outcomeWord(v.Outcome), v.countLabel(), note), file
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
	return fmt.Sprintf("repro: none: %s (slice of %s)", why, ref.chain), ""
}

func clearingRead(e *env, ref gateRef) string {
	rec, err := e.store.LatestRun(ref.chain)
	if err != nil || ref.it.Reason.Kind != reasonWrite {
		return ""
	}
	a := runAttribution(e, rec)
	at := a.index(ref.it.Step)
	if at < 0 {
		return ""
	}
	i, _ := a.lastMatch(rec.Steps[at], ref.it.Path)
	if i < 0 || len(a.entityWrites(at, ref.it.Path, a.bad, i)) == len(a.entityWrites(at, ref.it.Path, a.bad, -1)) {
		return ""
	}
	return rec.Steps[i].ID
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
	answered := r.Step + "_answered"
	back.Expect = []chain.Expectation{{Path: path, Equals: "${vars." + answered + "}"}}
	if slice.Vars == nil {
		slice.Vars = map[string]any{}
	}
	slice.Vars[answered] = r.Want
	slice.Steps = append(slice.Steps, &back)
	slice.Description = strings.TrimSpace(slice.Description) + fmt.Sprintf("\nThen %s reads %s back and expects what %s answered, %s: run it with -keep-going to see the answer and the stored value side by side.", r.Read, field, r.Step, valueText(r.Want))
	args := []string{"run", file, "-quiet", "-keep-going"}
	for _, v := range vars {
		args = append(args, "-var", v+"="+chain.NewRunTag())
	}
	if writeSliceFile(file, slice) == nil {
		items := gateExec(ctx, args).side.Items
		if slices.ContainsFunc(items, func(x gateItem) bool { return x.Step == r.Step }) && slices.ContainsFunc(items, func(x gateItem) bool { return x.Step == r.Read && x.Path == path }) {
			return " -keep-going", fmt.Sprintf("; the read-back %s (%s) reads %s=%s where the write answered %s", r.Read, chain.RPCName(read.Call), field, valueText(r.Got), valueText(r.Want))
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
		more = fmt.Sprintf(", leaving out %d inside whole lists a step marks volatile, which hold whatever else the backend holds (%s)", inLists, chain.ListSome(lists, 3))
	}
	if len(unread) > 0 {
		more += "; no record to read for " + chain.ListSome(unread, 5)
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
			fmt.Fprintf(os.Stderr, "gate: -repro: planning %s into .shrt/scratch/ and running it once, for the state(s) no chain calls it from\n", chain.RPCName(g.RPC))
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
		"in .shrt/scratch/ (%s). No safe spot covers these states, so a failure here is not comparable to an approved run; it fails what the contract and its plan expect, so treat it as a fault unless the contract is wrong:\n%s",
		len(gaps), min(len(gaps), gapProbes), time.Since(began).Round(time.Second), strings.Join(lines, "\n")), tally
}

func gapFile(rpc string) string {
	return strings.ToLower(strings.ReplaceAll(shortRPC(rpc), "/", "-")) + "-gaps.yaml"
}

func gapPlan(rpc string) string {
	return fmt.Sprintf("shrt contract plan %s -write %s (into .shrt/scratch/)", chain.RPCName(rpc), gapFile(rpc))
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
		err = fmt.Errorf("its plan leaves %d required field(s) without test data: shrt contract plan %s -notes", plan.UnfilledCount(), chain.RPCName(rpc))
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
		return []string{fmt.Sprintf("passes: %d call(s) from that state, %s  (shrt run %s)", len(calls), chain.ListSome(calls, 3), shown)}
	}
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
		line := fmt.Sprintf("%s: %d step(s) in 1 chain(s); e.g. %s %s", strings.TrimSpace(rpc+" "+chain.ListSome(fields, 3)), len(steps), r.c.Name, ex.Step)
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
		repro, file := r.repro(ctx, e, lib, g.RPC, calls, ex)
		if rpc == shortRPC(g.RPC) {
			if trigger, against := firmTrigger(ctx, e, file, gateRef{chain: r.c.Name, it: ex}, fails, passes); against != "" {
				out = append(out, "trigger: none: "+against)
			} else if trigger != "" {
				out = append(out, trigger)
			}
		}
		out = append(out, repro)
	}
	return out
}

type triggerProbe struct {
	label   string
	steps   []*chain.Step
	dims    map[string]string
	fail    bool
	changed []string
}

func firmTrigger(ctx context.Context, e *env, file string, at gateRef, fails, passes []rowCall) (string, string) {
	line, apart := triggerOf(fails, passes)
	c, err := chain.LoadFile(file)
	rec, rErr := e.store.LatestRun(at.chain)
	if err != nil || rErr != nil {
		return line, ""
	}
	base, _ := rec.Step(at.it.Step)
	t := slices.IndexFunc(c.Steps, func(s *chain.Step) bool { return s.ID == at.it.Step })
	i := slices.IndexFunc(fails, func(f rowCall) bool { return f.at == at.chain+" "+at.it.Step })
	if base == nil || t < 0 || fails != nil && (i < 0 || len(apart) != 1) {
		return line, ""
	}
	send := func(p triggerProbe, leaf string) (*runner.Record, *runner.StepRecord, bool, bool) {
		sc := &chain.Chain{APIVersion: c.APIVersion, Name: c.Name, Vars: c.Vars, Volatile: c.Volatile, Redact: c.Redact, Steps: p.steps}
		vars := map[string]any{}
		for _, name := range chain.FreshVars(sc.Steps, isLoginStep(e), nil) {
			vars[name] = chain.NewRunTag()
		}
		got, err := executeChain(ctx, e, sc, runner.Options{Vars: vars, Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, KeepGoing: true}, true)
		if err != nil {
			return nil, nil, false, false
		}
		for _, id := range p.changed {
			if cp, _ := got.Step(id); id != at.it.Step && (cp == nil || refusalOf(cp) != "") {
				return nil, nil, false, false
			}
		}
		st, _ := got.Step(at.it.Step)
		fail, known := failsLike(at.it, base, st, leaf)
		return got, st, fail, known
	}
	if fails == nil {
		p, key, sent := unknownIDProbe(c.Steps[:t+1], at.it, rec, base)
		if p == nil {
			return line, ""
		}
		if _, _, fail, known := send(*p, ""); known && fail {
			return fmt.Sprintf("trigger: fails for every unknown %s sent (2 calls: %s, %s)", key, sent, p.label), ""
		} else if known {
			return fmt.Sprintf("trigger: fails for the unknown %s sent (1 call: %s), not for another of its shape no record has (1 call: %s)", key, sent, p.label), ""
		}
		return line, ""
	}
	tag, _ := rec.Vars[chain.RunTagVar].(string)
	probes, leaf := triggerProbes(c.Steps[:t+1], at.it, fails[i], fails, passes, apart[0], tag), fails[i].leaf
	fails, passes = slices.Clone(fails), slices.Clone(passes)
	for _, p := range probes {
		got, st, fail, known := send(p, leaf)
		if !known {
			continue
		}
		if fail != p.fail {
			return "", "sent again " + p.label + ", the call " + map[bool]string{true: "failed", false: "passed"}[fail]
		}
		call := rowCall{at: c.Name + " " + p.label, call: fails[i].call, dims: p.dims, before: earlierProfiles(got, slices.Index(got.Steps, st), map[int]bool{})}
		if !fail {
			passes = append(passes, call)
			continue
		}
		if leaf != "" {
			g, _ := chain.Get(decoded(st.Response), at.it.Path)
			call.leaf, call.sent = leaf, sentOf(st, leaf)
			call.got, _ = g.(string)
		}
		fails = append(fails, call)
	}
	if firm, now := triggerOf(fails, passes); slices.Equal(now, apart) {
		return firm, ""
	}
	return line, ""
}

func failsLike(it gateItem, base, st *runner.StepRecord, leaf string) (bool, bool) {
	if st == nil || len(st.Response) == 0 && st.Transport == nil {
		return false, false
	}
	r, was := refusalOf(st), refusalOf(base)
	if verdictCode(it.Path) {
		return r == was, r == was || r == "" || was == ""
	}
	got, _ := chain.Get(decoded(st.Response), it.Path)
	if old, _ := chain.Get(decoded(base.Response), it.Path); r != "" || leaf == "" && !chain.IsZeroOf("", old) {
		return false, false
	}
	if leaf != "" {
		g, ok := got.(string)
		return g != sentOf(st, leaf), ok
	}
	return chain.IsZeroOf("", got), true
}

func triggerProbes(steps []*chain.Step, it gateItem, call rowCall, fails, passes []rowCall, split, tag string) []triggerProbe {
	kind, name, _ := strings.Cut(split, " ")
	t, thin := len(steps)-1, numCalls(fails) == 1 || numCalls(passes) == 1
	own := maps.Clone(call.dims)
	maps.DeleteFunc(own, func(k, _ string) bool {
		return k != split && (strings.HasPrefix(k, "num ") || strings.HasPrefix(k, "bytes "))
	})
	dims := func(k, v string) map[string]string {
		d := maps.Clone(own)
		if d[k] = v; v == "" && kind == "set" {
			delete(d, k)
		}
		return d
	}
	key, found := namecase.LookupKey(steps[t].Body, name)
	var out []triggerProbe
	switch kind {
	case "len", "repeat":
		j, items := -1, []any(nil)
		for _, id := range append([]string{steps[t].ID}, sentRefs(steps[t], steps)...) {
			if s := slices.IndexFunc(steps, func(s *chain.Step) bool { return s.ID == id }); s >= 0 && j < 0 {
				if k, ok := namecase.LookupKey(steps[s].Body, name); ok {
					j, key = s, k
					items, _ = steps[s].Body[k].([]any)
				}
			}
		}
		n := len(items)
		if !thin || !verdictCode(it.Path) || n == 0 || strconv.Itoa(n) != call.dims["len "+name] {
			return nil
		}
		lists := [][]any{append(slices.Clone(items), items[n-1])}
		fv, pv := numbers(dimValues(fails, "len "+name)), numbers(dimValues(passes, "len "+name))
		if n > 1 && (kind == "repeat" && repeatedKey(items[:n-1]) == "" || kind == "len" && fv[0] > slices.Max(pv) && float64(n-1) < fv[0]) {
			lists = append(lists, items[:n-1])
		}
		for x, l := range lists {
			d := dims("len "+name, strconv.Itoa(len(l)))
			if _, ok := d["repeat "+name]; ok {
				d["repeat "+name] = repeatedKey(l)
			}
			out = append(out, triggerProbe{label: "with " + name + " of " + plural(len(l), "item"), fail: x == 0, dims: d, changed: []string{steps[j].ID},
				steps: withStep(steps, j, func(s *chain.Step) { s.Body[key] = l })})
		}
	case "bytes":
		k, s := keptBytes(fails, name), call.sent
		if fresh := chain.NewRunTag(); tag != "" && len(fresh) == len(tag) {
			s = strings.ReplaceAll(s, tag, fresh)
		}
		fv, pv := numbers(dimValues(fails, split)), numbers(dimValues(passes, split))
		for _, n := range []int{k, k + 1} {
			if k < 0 || !found || call.leaf != name || len(s) < n || !utf8.ValidString(s[:n]) || n > k && fv[0] <= float64(n) || n == k && slices.Max(pv) >= float64(n) {
				continue
			}
			v := s[:n]
			out = append(out, triggerProbe{label: fmt.Sprintf("with %s of %d bytes", name, n), fail: n > k, dims: dims(split, strconv.Itoa(n)),
				steps: withStep(steps, t, func(st *chain.Step) { st.Body[key] = v })})
		}
	case "set":
		was, _ := steps[t].Body[key].(string)
		empty, wide := call.dims[split] == "", widePrefix(steps, name)
		switch {
		case !thin || strings.ContainsAny(name, ".["):
			return nil
		case empty && found && was == "":
			out = append(out, triggerProbe{label: "with " + name + " absent", fail: true, dims: dims(split, ""), steps: withStep(steps, t, func(s *chain.Step) { delete(s.Body, key) })})
		case empty != found:
			out = append(out, triggerProbe{label: "with " + name + ` ""`, fail: empty, dims: dims(split, ""), steps: withStep(steps, t, func(s *chain.Step) { s.Body[cmp.Or(key, name)] = "" })})
		default:
			return nil
		}
		if wide != "" {
			out = append(out, triggerProbe{label: fmt.Sprintf("with %s %q", name, wide), fail: !empty, dims: dims(split, "set"), steps: withStep(steps, t, func(s *chain.Step) { s.Body[cmp.Or(key, name)] = wide })})
		}
	case "as":
		bad, as := dimValues(fails, "as"), call.dims["as"]
		p := triggerProbe{label: "as " + as + " on what " + as + " created", fail: true, dims: own, steps: steps}
		for _, id := range sentRefs(steps[t], steps) {
			j := slices.IndexFunc(steps, func(s *chain.Step) bool { return s.ID == id })
			if j >= 0 && !chain.IsReadOnlyCall(steps[j].Call) && (steps[j].SkipAuth || cmp.Or(steps[j].Auth, config.DefaultAuthProfile) != as) {
				p.steps, p.changed = withStep(p.steps, j, func(s *chain.Step) {
					if s.Auth, s.SkipAuth = as, false; as == config.DefaultAuthProfile {
						s.Auth = ""
					}
				}), append(p.changed, id)
			}
		}
		if len(p.changed) > 0 && as != runner.NoAuthProfile && as != chain.InvalidTokenAuth && !slices.ContainsFunc(fails, func(f rowCall) bool {
			return len(f.before) == 0 || slices.ContainsFunc(bad, func(p string) bool { return f.before[p] })
		}) {
			out = append(out, p)
		}
	}
	return out
}

func unknownIDProbe(steps []*chain.Step, it gateItem, rec *runner.Record, base *runner.StepRecord) (*triggerProbe, string, string) {
	t, keys := len(steps)-1, []string{}
	for k := range steps[t].Body {
		if namecase.IDNamed(k) {
			keys = append(keys, k)
		}
	}
	if !verdictCode(it.Path) || refusalOf(base) != "" || !chain.IsReadOnlyCall(steps[t].Call) || len(keys) != 1 {
		return nil, "", ""
	}
	sent, known := sentOf(base, keys[0]), false
	for _, st := range rec.Steps[:max(slices.Index(rec.Steps, base), 0)] {
		if st != nil {
			eachLeaf(decoded(st.Response), "", func(_ string, v any) { known = known || compactValue(v) == sent })
		}
	}
	v := freshOfShape(sent)
	if sent == "" || v == sent || known {
		return nil, "", ""
	}
	return &triggerProbe{label: v, steps: withStep(steps, t, func(s *chain.Step) { s.Body[keys[0]] = v }), fail: true}, keys[0], sent
}

func withStep(steps []*chain.Step, j int, change func(*chain.Step)) []*chain.Step {
	out := slices.Clone(steps)
	cp := *out[j]
	if cp.Body = maps.Clone(cp.Body); cp.Body == nil {
		cp.Body = map[string]any{}
	}
	change(&cp)
	out[j] = &cp
	return out
}

var digitToken = regexp.MustCompile(`[0-9A-Za-z]*[0-9][0-9A-Za-z]*`)

func freshOfShape(s string) string {
	return digitToken.ReplaceAllStringFunc(s, func(tok string) string {
		return strings.Map(func(r rune) rune {
			for _, set := range []string{"0123456789", "abcdef", "ABCDEF"} {
				if strings.ContainsRune(set, r) {
					return rune(set[rand.IntN(len(set))])
				}
			}
			return r
		}, tok)
	})
}

func widePrefix(steps []*chain.Step, field string) string {
	lower := strings.ToLower(field)
	of := strings.Trim(strings.ReplaceAll(lower, "prefix", ""), "_")
	for _, s := range steps {
		if k, ok := namecase.LookupKey(s.Body, of); ok && of != lower {
			if v, _ := s.Body[k].(string); v != "" && v[0] != '$' {
				return string([]rune(v)[:1])
			}
		}
	}
	return ""
}

func keptBytes(fails []rowCall, leaf string) int {
	k := -1
	for _, f := range fails {
		if f.leaf != leaf {
			continue
		}
		if k >= 0 && len(f.got) != k || !strings.HasPrefix(f.sent, f.got) && !strings.HasSuffix(f.sent, f.got) {
			return -1
		}
		k = len(f.got)
	}
	return k
}

func sentOf(st *runner.StepRecord, field string) string {
	m, _ := decoded(st.Request).(map[string]any)
	k, _ := namecase.LookupKey(m, field)
	s, _ := m[k].(string)
	return s
}

func sentRefs(st *chain.Step, in []*chain.Step) []string {
	var out []string
	for _, ref := range st.SendReferences() {
		if id, ok := chain.ParseRef(ref).StepID(); ok && slices.ContainsFunc(in, func(s *chain.Step) bool { return s.ID == id }) {
			out = append(out, id)
		}
	}
	return out
}

func (r *gapRun) repro(ctx context.Context, e *env, lib *contract.Library, rpc string, own []string, ex gateItem) (string, string) {
	need, drop := map[string]bool{}, []string{}
	for queue := slices.Clone(own); len(queue) > 0; queue = queue[1:] {
		if st, found := r.c.Step(queue[0]); found && !need[st.ID] {
			need[st.ID], queue = true, append(queue, sentRefs(st, r.c.Steps)...)
		}
	}
	for _, st := range r.c.Steps {
		if m, err := e.cat.Lookup(st.Call); err == nil && m.FullName == rpc && !need[st.ID] {
			by := slices.DeleteFunc(sentRefs(st, r.c.Steps), func(id string) bool { return need[id] })
			if len(by) == 0 {
				by = []string{st.ID}
			}
			drop = append(drop, by...)
		}
	}
	fresh, focus := freshVarsOf(e, lib), filepath.Join(scratchDir(e), "focus", filepath.Base(r.file))
	if res, err := chain.Without(r.c, drop, r.c.Name); err == nil && writeSliceFile(focus, res.Chain) == nil {
		defer os.RemoveAll(filepath.Dir(focus))
		if line, file := reproRow(ctx, e, gateRef{chain: focus, it: ex}, fresh); file != "" {
			return line, file
		}
	}
	return reproRow(ctx, e, gateRef{chain: r.file, it: ex}, fresh)
}
