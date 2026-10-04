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
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/namecase"
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
		if line := settleRow(ctx, e, gr, spot, wait); line != "" {
			fmt.Println("    " + line)
		}
		at := gr.reproAt(spot)
		key := at.chain + " " + at.it.Step
		if done[key] == "" {
			done[key] = reproRow(ctx, e, at, fresh)
		}
		fmt.Println("    " + done[key])
	}
	fmt.Println(gateMasks(ctx, chains))
}

func (gr *gateGroup) reproAt(spot map[string]bool) gateRef {
	at, rank := gateRef{chain: gr.in, it: gr.example}, -1
	if spot[gr.in] {
		return at
	}
	for _, r := range gr.refs {
		if spot[r.chain] && r.it.groupRank() > rank {
			at, rank = r, r.it.groupRank()
		}
	}
	return at
}

func scratchDir(e *env) string {
	return e.cfg.Abs(filepath.Join(config.DirName, "scratch"))
}

func settleRow(ctx context.Context, e *env, gr *gateGroup, spot map[string]bool, wait time.Duration) string {
	for _, onSpot := range []bool{true, false} {
		for _, ref := range gr.refs {
			if spot[ref.chain] != onSpot || ref.it.Reason.Read != ref.it.Step {
				continue
			}
			if _, via, _ := tellApartReads(e, ref.it.Reason, ref.it.Path); len(via) > 0 {
				return settle(ctx, e, ref, via[0], wait)
			}
		}
	}
	return ""
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
	file := filepath.Join(scratchDir(e), ref.chain+"-slice-"+step+".yaml")
	args := []string{"chain", "slice", ref.chain, "-step", step, "-run", "latest", "-verify", "-write=" + file, "-json"}
	if c, err := e.resolveChain(ref.chain); err == nil {
		for _, v := range fresh(c, step) {
			args = append(args, "-var", v+"="+chain.NewRunTag())
		}
	}
	v, written, why := sliceRepro(ctx, args)
	if next := strings.Fields(nextOf(v)); len(next) > 1 && next[0] == "shrt" {
		if w, wr, _ := sliceRepro(ctx, append(next[1:], "-json")); w != nil {
			v, written = w, wr
		}
	}
	switch {
	case v == nil:
		return "repro: none: " + why
	case v.Outcome == sliceReproduced || v.Outcome == sliceIntermittent:
		return fmt.Sprintf("repro: shrt run %s  (%s%s)", shownPath(cmp.Or(written, file)), outcomeWord(v.Outcome), v.countLabel())
	}
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

func nextOf(v *sliceVerdict) string {
	if v == nil || v.Outcome == sliceReproduced {
		return ""
	}
	return strings.ReplaceAll(v.Next, "<fresh>", chain.NewRunTag())
}

func sliceRepro(ctx context.Context, args []string) (*sliceVerdict, string, string) {
	out := gateExec(ctx, args)
	var res struct {
		Written string        `json:"written"`
		Verify  *sliceVerdict `json:"verify"`
	}
	if json.Unmarshal([]byte(out.stdout), &res) != nil || res.Verify == nil {
		return nil, "", lastLine(out)
	}
	return res.Verify, res.Written, ""
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
