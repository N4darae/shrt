package main

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/runner"
)

func loadGateRuns(e *env, chains []*gateChain, since time.Time) {
	from := since.UTC().Truncate(time.Second)
	for _, g := range chains {
		if g.skipped {
			continue
		}
		ids, err := e.store.ListRuns(g.name)
		if err != nil {
			continue
		}
		kinds := map[string]bool{}
		for i := len(ids) - 1; i >= 0; i-- {
			stamp, _, _ := strings.Cut(ids[i], "-")
			if at, err := time.Parse("20060102T150405Z", stamp); err != nil || at.Before(from) {
				break
			}
			rec, err := e.store.LoadRun(g.name, ids[i])
			if err == nil && rec.StartedAt.Before(since) {
				break
			}
			if err == nil && !kinds[runKind(rec)] {
				kinds[runKind(rec)] = true
				g.runs = append(g.runs, rec)
			}
		}
	}
}

func (g *gateChain) runOf(kind string) *runner.Record {
	for _, rec := range g.runs {
		if runKind(rec) == kind {
			return rec
		}
	}
	return &runner.Record{}
}

type rowCall struct {
	at   string
	dims map[string]string
}

func requestDims(st *runner.StepRecord) map[string]string {
	dims := map[string]string{"as": profileOf(st)}
	var walk func(v any, at string)
	walk = func(v any, at string) {
		m, _ := v.(map[string]any)
		for k, x := range m {
			p := strings.TrimPrefix(at+"."+k, ".")
			switch t := x.(type) {
			case []any:
				dims["len "+p] = strconv.Itoa(len(t))
				dims["repeat "+p] = repeatedKey(t)
			case map[string]any:
				walk(t, p)
			case nil:
			default:
				if fmt.Sprint(t) != "" && !namecase.IDNamed(k) {
					dims["set "+p] = "set"
				}
			}
		}
	}
	walk(decoded(st.Request), "")
	return dims
}

func repeatedKey(items []any) string {
	seen := map[string]bool{}
	for _, it := range items {
		m, _ := it.(map[string]any)
		for _, k := range sortedKeys(m) {
			if v := compactValue(m[k]); namecase.IDNamed(k) && v != "" {
				if seen[k+"\x00"+v] {
					return k
				}
				seen[k+"\x00"+v] = true
			}
		}
	}
	return ""
}

func dimValue(dims map[string]string, k string) string {
	if v, ok := dims[k]; ok {
		return v
	}
	if strings.HasPrefix(k, "len ") {
		return "0"
	}
	return ""
}

func differsOnlyIn(a, b map[string]string, keys []string) bool {
	for _, k := range keysOfBoth(a, b) {
		if !slices.Contains(keys, k) && dimValue(a, k) != dimValue(b, k) {
			return false
		}
	}
	return true
}

func (gr *gateGroup) callOf(ref gateRef, byName map[string]*gateChain) *runner.StepRecord {
	rpc, _, _ := strings.Cut(gr.rpc, " ")
	g := byName[ref.chain]
	if g == nil {
		return nil
	}
	rec, ids := g.runOf(ref.it.from), []string{ref.it.Reason.Step, ref.it.Step, ref.it.Reason.Read}
	for _, o := range ref.it.Reason.Or {
		ids = append(ids, o.Step)
	}
	for _, id := range ids {
		if st, found := rec.Step(id); id != "" && found && st != nil && shortRPC(st.Call) == rpc {
			return st
		}
	}
	return nil
}

func (gr *gateGroup) rowCalls(byName map[string]*gateChain) (fails, passes []rowCall) {
	rpc, _, _ := strings.Cut(gr.rpc, " ")
	failing, refused, paths, own, later := map[string]bool{}, map[string]bool{}, []string{}, false, false
	for _, ref := range gr.refs {
		st := gr.callOf(ref, byName)
		if st == nil {
			continue
		}
		key := ref.chain + " " + st.ID
		if !failing[key] {
			failing[key] = true
			fails = append(fails, rowCall{at: key, dims: requestDims(st)})
		}
		if it := ref.it; it.Reason.Kind != reasonKnockOn && it.Step == st.ID && verdictCode(it.Path) {
			if it.Rule == "not_equal" || !slices.Contains([]string{chain.EnvelopeOK(), chain.TransportOK, "<none>", ""}, it.Want) {
				return nil, nil
			}
			refused[key] = true
		}
	}
	for _, ref := range gr.refs {
		st := gr.callOf(ref, byName)
		if st == nil || ref.it.Reason.Kind == reasonKnockOn || refused[ref.chain+" "+st.ID] {
			continue
		}
		for _, p := range []string{ref.it.Path, ref.it.Reason.Path} {
			if p == "" || strings.HasPrefix(p, "(") || p == "step" || verdictField(p) {
				continue
			}
			own, later = own || st.ID == ref.it.Step, later || st.ID != ref.it.Step
			if !slices.Contains(paths, p) {
				paths = append(paths, p)
			}
		}
	}
	verdictRow := len(paths) == 0 && len(refused) > 0
	if len(paths) == 0 && !verdictRow {
		return nil, nil
	}
	for _, g := range byName {
		for _, rec := range g.runs {
			var a attribution
			writes := map[string][]int{}
			readBack := func(j int, path string) []int {
				at := fmt.Sprint(j, " ", path)
				if _, ok := writes[at]; !ok {
					if a.rec == nil {
						a = runAttribution(nil, rec)
					}
					writes[at] = a.entityWrites(j, path, nil, -1)
				}
				return writes[at]
			}
			for i, st := range rec.Steps {
				if st == nil || shortRPC(st.Call) != rpc || failing[g.name+" "+st.ID] || st.Status != runner.StatusPassed || refusalOf(st) != "" ||
					profileOf(st) == runner.NoAuthProfile || profileOf(st) == chain.InvalidTokenAuth {
					continue
				}
				checked := verdictRow || own && slices.ContainsFunc(st.Expect, func(ex chain.ExpectResult) bool { return checks(ex, paths) })
				for j := i + 1; !checked && later && isWrite(st) && j < len(rec.Steps); j++ {
					r := rec.Steps[j]
					if r == nil || isWrite(r) || r.Status != runner.StatusPassed {
						continue
					}
					for _, ex := range r.Expect {
						checked = checked || checks(ex, paths) && slices.Contains(readBack(j, ex.Path), i)
					}
				}
				if checked {
					passes = append(passes, rowCall{at: g.name + " " + st.ID, dims: requestDims(st)})
				}
			}
		}
	}
	slices.SortFunc(passes, func(x, y rowCall) int { return strings.Compare(x.at, y.at) })
	return fails, passes
}

func verdictField(p string) bool {
	return chain.IsEnvelopePath(p) || chain.IsTransportPath(p) || p == "code"
}

func verdictCode(p string) bool {
	return p == chain.EnvelopePath() || p == chain.TransportPrefix+".code" || p == "code"
}

func checks(ex chain.ExpectResult, paths []string) bool {
	if !ex.Passed || ex.Rule == "unevaluated" {
		return false
	}
	got := gateIndex.ReplaceAllString(ex.Path, "[]$1")
	return slices.ContainsFunc(paths, func(p string) bool {
		p = gateIndex.ReplaceAllString(p, "[]$1")
		return namecase.Equal(leafOf(ex.Path), leafOf(p)) || strings.HasPrefix(got, p+".") || strings.HasPrefix(got, p+"[]")
	})
}

func triggerOf(fails, passes []rowCall) (string, []string) {
	if len(fails) == 0 || len(passes) == 0 {
		return "", nil
	}
	keys := map[string]bool{}
	for _, c := range append(slices.Clone(fails), passes...) {
		for k := range c.dims {
			keys[k] = true
		}
	}
	for _, f := range fails {
		for _, p := range passes {
			if differsOnlyIn(f.dims, p.dims, nil) {
				return "", nil
			}
		}
	}
	var apart []string
	for _, k := range sortedKeys(keys) {
		fv, pv := dimValues(fails, k), dimValues(passes, k)
		list, repeat := strings.CutPrefix(k, "repeat ")
		switch {
		case repeat && (slices.Contains(fv, "") || len(pv) != 1 || pv[0] != "" || slices.Max(numbers(dimValues(passes, "len "+list))) < 2):
		case slices.ContainsFunc(fv, func(v string) bool { return slices.Contains(pv, v) }):
		case strings.HasPrefix(k, "len ") && !(numbers(fv)[0] > slices.Max(numbers(pv)) || slices.Max(numbers(fv)) < numbers(pv)[0]):
		default:
			apart = append(apart, k)
		}
	}
	apart = slices.DeleteFunc(apart, func(k string) bool {
		list, isLen := strings.CutPrefix(k, "len ")
		return isLen && slices.Contains(apart, "repeat "+list)
	})
	if len(apart) == 0 {
		return "", nil
	}
	rank := func(k string) int {
		for i, p := range []string{"as", "repeat ", "set ", "len "} {
			if strings.HasPrefix(k, p) {
				return i
			}
		}
		return 4
	}
	slices.SortStableFunc(apart, func(x, y string) int { return cmp.Compare(rank(x), rank(y)) })
	return "trigger: fails " + triggerSide(fails, passes, apart) + "; passes " + triggerSide(passes, fails, apart), apart
}

func dimValues(calls []rowCall, k string) []string {
	var out []string
	for _, c := range calls {
		if v := dimValue(c.dims, k); !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

func numbers(vals []string) []int {
	out := make([]int, 0, len(vals))
	for _, v := range vals {
		n, _ := strconv.Atoi(v)
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

func triggerSide(calls, other []rowCall, keys []string) string {
	var parts, lengths []string
	for _, k := range keys {
		vals := dimValues(calls, k)
		kind, name, _ := strings.Cut(k, " ")
		switch kind {
		case "as":
			parts = append(parts, "as "+andList(vals))
		case "set":
			if vals[0] == "set" {
				parts = append(parts, "with "+name+" set")
			} else {
				parts = append(parts, "with "+name+" empty or absent")
			}
		case "repeat":
			if vals[0] == "" {
				parts = append(parts, "with distinct "+strings.Join(dimValues(other, k), " or "))
			} else {
				parts = append(parts, "when "+name+" repeat "+strings.Join(vals, " or "))
			}
			var ns []string
			for _, n := range numbers(dimValues(calls, "len "+name)) {
				ns = append(ns, strconv.Itoa(n))
			}
			unit := " items"
			if len(ns) == 1 && ns[0] == "1" {
				unit = " item"
			}
			lengths = append(lengths, name+" of "+andList(ns)+unit)
		case "len":
			ns, os := numbers(vals), numbers(dimValues(other, k))
			switch {
			case ns[0] > os[len(os)-1]:
				parts = append(parts, fmt.Sprintf("with %s of %d+ items", name, ns[0]))
			case len(ns) == 1 && ns[0] == 0:
				parts = append(parts, "with no "+name)
			case len(ns) == 1:
				parts = append(parts, fmt.Sprintf("with %s of %s", name, plural(ns[0], "item")))
			default:
				parts = append(parts, fmt.Sprintf("with %s of up to %d items", name, ns[len(ns)-1]))
			}
		}
	}
	n := map[string]bool{}
	for _, c := range calls {
		n[c.at] = true
	}
	return strings.Join(parts, " ") + " (" + strings.Join(append([]string{plural(len(n), "call")}, lengths...), "; ") + ")"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func andList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

func (gr *gateGroup) pick(byName map[string]*gateChain, fails, passes []rowCall, apart []string, size func(name, step string) int) {
	dims, shapes := map[string]map[string]string{}, map[string]int{}
	for _, f := range fails {
		dims[f.at] = f.dims
		shapes[shapeKey(f.dims)]++
	}
	rank := func(r gateRef) []int {
		g, paired, typical := byName[r.chain], 0, 0
		if st := gr.callOf(r, byName); st != nil {
			d := dims[r.chain+" "+st.ID]
			if len(apart) == 0 {
				typical = shapes[shapeKey(d)]
			}
			if len(apart) > 0 && slices.ContainsFunc(passes, func(p rowCall) bool { return differsOnlyIn(d, p.dims, apart) }) {
				paired = 1
			}
		}
		at := 0
		if g.spot {
			at += 2
		}
		if g.keptRed == "" {
			at++
		}
		return []int{at, paired, r.it.groupRank(g.spot), typical}
	}
	best, top := -1, []int(nil)
	for i, r := range gr.refs {
		rk := rank(r)
		c := slices.Compare(rk, top)
		if best < 0 || c > 0 || c == 0 && size(r.chain, r.it.Step) < size(gr.refs[best].chain, gr.refs[best].it.Step) {
			best, top = i, rk
		}
	}
	gr.in, gr.example = gr.refs[best].chain, gr.refs[best].it
}

func shapeKey(dims map[string]string) string {
	var out []string
	for _, k := range sortedKeys(dims) {
		out = append(out, k+"="+dims[k])
	}
	return strings.Join(out, "\x00")
}

func sliceSizes(e *env) func(name, step string) int {
	sizes, chains := map[string]int{}, map[string]*chain.Chain{}
	var opts *chain.SliceOptions
	return func(name, step string) int {
		if n, ok := sizes[name+" "+step]; ok || e == nil {
			return n
		}
		if opts == nil {
			opts = &chain.SliceOptions{RPCOf: rpcOf(e), IsLogin: isLoginStep(e)}
			if lib, err := e.library(); err == nil {
				opts.Prereqs, opts.KeyField = contract.PrereqsFor(lib), contract.KeyFieldFor(lib)
			}
		}
		if _, ok := chains[name]; !ok {
			chains[name], _ = e.resolveChain(name)
		}
		n := 1 << 20
		if c := chains[name]; c != nil {
			if res, err := chain.Slice(c, step, *opts); err == nil {
				n = len(res.Kept)
			}
		}
		sizes[name+" "+step] = n
		return n
	}
}
