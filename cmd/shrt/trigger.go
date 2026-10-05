package main

import (
	"cmp"
	"fmt"
	"maps"
	"math"
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
	at, call        string
	dims            map[string]string
	before          map[string]bool
	leaf, sent, got string
}

func flatRequest(v any, at string, out map[string]any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			flatRequest(x, strings.TrimPrefix(at+"."+k, "."), out)
		}
	case []any:
		out[at] = t
		if len(t) > 0 {
			flatRequest(t[0], at+"[first]", out)
			flatRequest(t[len(t)-1], at+"[last]", out)
		}
	default:
		out[at] = t
	}
	return out
}

func fieldOf(path string) string {
	k, _, _ := strings.Cut(path[strings.LastIndex(path, ".")+1:], "[")
	return k
}

func requestDims(st *runner.StepRecord) map[string]string {
	dims := map[string]string{"as": profileOf(st)}
	for p, v := range flatRequest(decoded(st.Request), "", map[string]any{}) {
		s := fmt.Sprint(v)
		if list, ok := v.([]any); ok {
			dims["len "+p], dims["repeat "+p] = strconv.Itoa(len(list)), repeatedKey(list)
			continue
		}
		if v == nil || s == "" || namecase.IDNamed(fieldOf(p)) {
			continue
		}
		dims["set "+p] = "set"
		if n, ok := number(v); ok && !math.IsInf(n, 0) && !math.IsNaN(n) {
			dims["num "+p] = s
		} else if _, ok := v.(string); ok {
			dims["bytes "+p] = strconv.Itoa(len(s))
		}
	}
	return dims
}

func earlierProfiles(rec *runner.Record, at int, seen map[int]bool) map[string]bool {
	out := map[string]bool{}
	for id := range stepRefs(rec, at) {
		if j := slices.IndexFunc(rec.Steps[:at], func(st *runner.StepRecord) bool { return st != nil && st.ID == id }); j >= 0 && !seen[j] {
			seen[j], out[profileOf(rec.Steps[j])] = true, true
			maps.Copy(out, earlierProfiles(rec, j, seen))
		}
	}
	return out
}

func echoOf(st *runner.StepRecord, rec *runner.Record, it gateItem) (string, string, string) {
	read, found := rec.Step(it.Step)
	if !found || read == nil || verdictField(it.Path) {
		return "", "", ""
	}
	got, _ := chain.Get(decoded(read.Response), it.Path)
	g, _ := got.(string)
	for p, v := range flatRequest(decoded(st.Request), "", map[string]any{}) {
		s, _ := v.(string)
		if w, gw := gatePair(s, g); g != "" && s != g && namecase.Equal(fieldOf(p), leafOf(it.Path)) && w == it.Want && gw == it.Got {
			return fieldOf(p), s, g
		}
	}
	return "", "", ""
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
	failing, refused, paths, own, later := map[string]int{}, map[string]bool{}, []string{}, false, false
	for _, ref := range gr.refs {
		st := gr.callOf(ref, byName)
		if st == nil {
			continue
		}
		key, rec := ref.chain+" "+st.ID, byName[ref.chain].runOf(ref.it.from)
		if _, ok := failing[key]; !ok {
			failing[key] = len(fails)
			fails = append(fails, rowCall{at: key, call: chain.RPCName(st.Call), dims: requestDims(st), before: earlierProfiles(rec, slices.Index(rec.Steps, st), map[int]bool{})})
		}
		if f := &fails[failing[key]]; f.leaf == "" {
			f.leaf, f.sent, f.got = echoOf(st, rec, ref.it)
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
				_, failed := failing[g.name+" "+st.ID]
				if st == nil || shortRPC(st.Call) != rpc || failed || st.Status != runner.StatusPassed || refusalOf(st) != "" ||
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
					passes = append(passes, rowCall{at: g.name + " " + st.ID, call: chain.RPCName(st.Call), dims: requestDims(st), before: earlierProfiles(rec, i, map[int]bool{})})
				}
			}
		}
	}
	slices.SortFunc(passes, func(x, y rowCall) int { return strings.Compare(x.at, y.at) })
	for _, c := range append(slices.Clone(fails), passes...) {
		for k := range c.dims {
			if p, ok := strings.CutPrefix(k, "bytes "); ok && !slices.ContainsFunc(fails, func(f rowCall) bool { return f.leaf == fieldOf(p) }) {
				delete(c.dims, k)
			}
		}
	}
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

type cut struct {
	key   string
	below bool
	bound float64
	vals  []string
}

func (c cut) holds(call rowCall) bool {
	if c.vals != nil {
		return slices.Contains(c.vals, dimValue(call.dims, c.key))
	}
	n, ok := number(dimValue(call.dims, c.key))
	return ok && (c.below && n <= c.bound || !c.below && n >= c.bound)
}

func triggerOf(fails, passes []rowCall) (string, []string) {
	echo := echoRelation(fails)
	switch {
	case len(fails) == 0, len(passes) == 0 && numCalls(fails) < 2:
		return echo, nil
	case len(passes) == 0:
		return strings.TrimSuffix("trigger: "+everyCall(fails)+"; "+echo, "; "), nil
	}
	all, keys := append(slices.Clone(fails), passes...), []string{}
	for _, c := range all {
		for k := range c.dims {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	rank := func(k string) int {
		return slices.IndexFunc([]string{"as", "repeat ", "set ", "len ", "num ", "bytes "}, func(p string) bool { return strings.HasPrefix(k, p) })
	}
	slices.SortFunc(keys, func(x, y string) int { return cmp.Or(cmp.Compare(rank(x), rank(y)), strings.Compare(x, y)) })
	var cuts []cut
	for _, k := range keys {
		kind, list, _ := strings.Cut(k, " ")
		fv, pv := dimValues(fails, k), dimValues(passes, k)
		switch {
		case kind == "repeat" && (slices.Contains(fv, "") || len(pv) != 1 || pv[0] != "" || slices.Max(numbers(dimValues(passes, "len "+list))) < 2):
		case kind == "as" || kind == "set" || kind == "repeat":
			cuts = append(cuts, cut{key: k, vals: fv})
		case kind == "len" || len(fails) > 1 && len(passes) > 1 && !slices.ContainsFunc(all, func(c rowCall) bool { return c.dims[k] == "" }):
			ns := numbers(fv)
			cuts = append(cuts, cut{key: k, bound: ns[0]}, cut{key: k, below: true, bound: ns[len(ns)-1]})
		}
	}
	var apart []string
	var open []cut
	var pairs [][2]cut
	seen := map[string]bool{}
	for _, c := range cuts {
		sig := fmt.Sprint(c.below)
		for _, x := range all {
			sig += "\x00" + dimValue(x.dims, c.key)
		}
		switch {
		case !slices.ContainsFunc(passes, c.holds):
			apart = append(apart, c.key)
		case !seen[sig]:
			seen[sig], open = true, append(open, c)
		}
	}
	for i, a := range open {
		for _, b := range open[i+1:] {
			if a.key != b.key && !slices.ContainsFunc(passes, func(p rowCall) bool { return a.holds(p) && b.holds(p) }) {
				pairs = append(pairs, [2]cut{a, b})
			}
		}
	}
	apart = slices.DeleteFunc(slices.Compact(apart), func(k string) bool {
		list, isLen := strings.CutPrefix(k, "len ")
		return isLen && slices.Contains(apart, "repeat "+list)
	})
	line := ""
	switch {
	case len(apart) > 0 && len(pairs) == 0:
		failNote, passNote := "", ""
		if apart[0] == "as" {
			failNote, passNote = ownProfile(fails, passes)
		}
		failSide := triggerSide(fails, passes, apart, failNote)
		if failNote+passNote != "" {
			failSide = strings.Replace(failSide, "as ", "when "+fails[0].call+" is sent as ", 1)
		}
		line = "trigger: fails " + failSide + "; passes " + triggerSide(passes, fails, apart, passNote)
	case len(apart) == 0 && len(pairs) == 1:
		a, b := pairs[0][0], pairs[0][1]
		notA := slices.DeleteFunc(slices.Clone(passes), a.holds)
		notB := slices.DeleteFunc(slices.Clone(passes), func(p rowCall) bool { return !a.holds(p) || b.holds(p) })
		bare := func(calls, other []rowCall, k string) string {
			p, _ := dimPhrase(calls, other, k)
			return strings.TrimPrefix(p, "with ")
		}
		fa, _ := dimPhrase(fails, notA, a.key)
		line = fmt.Sprintf("trigger: fails %s and %s (%s); passes otherwise: %s (%s), %s (%s)", fa, bare(fails, notB, b.key), callCount(fails),
			bare(notA, fails, a.key), callCount(notA), bare(notB, fails, b.key), callCount(notB))
		apart = []string{a.key, b.key}
	default:
		return echo, nil
	}
	if echo != "" {
		line += "; " + echo
	}
	return line, apart
}

func ownProfile(fails, passes []rowCall) (string, string) {
	bad, used, failNote, passNote := dimValues(fails, "as"), map[string]bool{}, "", ""
	after := func(c rowCall) bool { return slices.ContainsFunc(bad, func(p string) bool { return c.before[p] }) }
	if n := slices.DeleteFunc(slices.Clone(fails), func(c rowCall) bool { return len(c.before) == 0 || after(c) }); len(n) > 0 {
		for _, c := range n {
			maps.Copy(used, c.before)
		}
		failNote = share(n, fails) + andList(sortedKeys(used))
		if own := slices.DeleteFunc(slices.Clone(fails), func(c rowCall) bool { return !after(c) }); len(own) > 0 {
			failNote += ", " + share(own, fails) + andList(bad)
		}
	}
	if n := slices.DeleteFunc(slices.Clone(passes), func(c rowCall) bool { return !after(c) }); len(n) > 0 {
		passNote = share(n, passes) + andList(bad)
	}
	return failNote, passNote
}

func share(some, all []rowCall) string {
	switch k, n := numCalls(some), numCalls(all); {
	case k < n:
		return fmt.Sprintf("%d on records created as ", k)
	case n > 1:
		return "all on records created as "
	}
	return "on records created as "
}

func everyCall(fails []rowCall) string {
	n, span := numCalls(fails), []string{}
	if as := dimValues(fails, "as"); as[0] != "" {
		span = append(span, "as "+andList(as))
	}
	keys := map[string]bool{}
	for _, c := range fails {
		for k := range c.dims {
			keys[k] = true
		}
	}
	for _, k := range sortedKeys(keys) {
		if list, ok := strings.CutPrefix(k, "len "); ok && !strings.Contains(list, "[") {
			ns := numbers(dimValues(fails, k))
			of := shown(ns[0])
			if ns[0] != ns[len(ns)-1] {
				of += " to " + shown(ns[len(ns)-1])
			}
			span = append(span, countOf(list, of))
		}
	}
	return strings.TrimSuffix(fmt.Sprintf("fails on every call (%d of %d; %s", n, n, strings.Join(span, ", ")), "; ") + ")"
}

func echoRelation(fails []rowCall) string {
	echoes := slices.DeleteFunc(slices.Clone(fails), func(f rowCall) bool { return f.leaf == "" })
	if len(echoes) == 0 || slices.ContainsFunc(echoes, func(f rowCall) bool { return f.leaf != echoes[0].leaf }) {
		return ""
	}
	every := func(rule func(s, g string) bool) bool {
		return !slices.ContainsFunc(echoes, func(f rowCall) bool { return !rule(f.sent, f.got) })
	}
	same := func(n func(f rowCall) int) int {
		if slices.ContainsFunc(echoes, func(f rowCall) bool { return n(f) != n(echoes[0]) }) {
			return -1
		}
		return n(echoes[0])
	}
	sent, count := "the "+echoes[0].leaf+" sent", " ("+callCount(echoes)+")"
	for _, r := range []struct {
		how  string
		fits func(s, g string) bool
	}{
		{" with its outer spaces trimmed", func(s, g string) bool { return g == strings.TrimSpace(s) }},
		{" in upper case", func(s, g string) bool { return g == strings.ToUpper(s) }},
		{" in lower case", func(s, g string) bool { return g == strings.ToLower(s) }},
		{" in another case", strings.EqualFold},
	} {
		if every(r.fits) {
			return "got is " + sent + r.how + count
		}
	}
	kept, dropped := same(func(f rowCall) int { return len(f.got) }), same(func(f rowCall) int { return len(f.sent) - len(f.got) })
	for _, end := range []struct {
		keep, drop string
		fits       func(s, g string) bool
	}{{"first", "last", strings.HasPrefix}, {"last", "first", strings.HasSuffix}} {
		if kept < 0 && dropped < 0 || !every(end.fits) {
			continue
		}
		switch {
		case dropped < 0:
			return fmt.Sprintf("got keeps the %s %s of %s%s", end.keep, plural(kept, "byte"), sent, count)
		case kept < 0:
			return fmt.Sprintf("got drops the %s %s of %s%s", end.drop, plural(dropped, "byte"), sent, count)
		}
		return fmt.Sprintf("got keeps the %s %d of the %d bytes of %s%s", end.keep, kept, kept+dropped, sent, count)
	}
	return ""
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

func numbers(vals []string) []float64 {
	out := make([]float64, 0, len(vals))
	for _, v := range vals {
		n, _ := number(v)
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

func shown(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}

func numCalls(of []rowCall) int {
	n := map[string]bool{}
	for _, c := range of {
		n[c.at] = true
	}
	return len(n)
}

func callCount(of []rowCall) string {
	return plural(numCalls(of), "call")
}

func triggerSide(calls, other []rowCall, keys []string, notes ...string) string {
	parts, extra := []string{}, []string{callCount(calls)}
	for _, k := range keys {
		p, x := dimPhrase(calls, other, k)
		parts, extra = append(parts, p), append(extra, x)
		if ns := numbers(dimValues(calls, k)); strings.HasPrefix(k, "len ") && len(ns) > 1 {
			var each []string
			for _, n := range ns {
				each = append(each, shown(n))
			}
			extra[0] += ": " + countOf(strings.TrimPrefix(k, "len "), strings.Join(each, ", "))
		}
	}
	return strings.Join(parts, " ") + " (" + strings.Join(slices.DeleteFunc(append(extra, notes...), func(x string) bool { return x == "" }), "; ") + ")"
}

func dimPhrase(calls, other []rowCall, k string) (string, string) {
	vals := dimValues(calls, k)
	kind, name, _ := strings.Cut(k, " ")
	switch kind {
	case "as":
		return "as " + andList(vals), ""
	case "set":
		if vals[0] == "set" {
			return "with " + name + " set", ""
		}
		return "with " + name + " empty or absent", ""
	case "repeat":
		var ns []string
		for _, n := range numbers(dimValues(calls, "len "+name)) {
			ns = append(ns, shown(n))
		}
		lengths := countOf(name, andList(ns))
		if vals[0] == "" {
			return "with distinct " + strings.Join(dimValues(other, k), " or "), lengths
		}
		return "when " + name + " repeat " + strings.Join(vals, " or "), lengths
	}
	ns, top := numbers(vals), slices.Max(numbers(dimValues(other, k)))
	lo, hi := ns[0], ns[len(ns)-1]
	of, unit := " of ", map[string]string{"len": " items", "bytes": " bytes"}[kind]
	if kind == "num" {
		of = " "
	}
	switch {
	case kind == "len" && hi == 0:
		return "with no " + name, ""
	case kind == "len" && lo > top:
		return "with " + countOf(name, shown(lo)+"+"), ""
	case kind == "len" && lo == hi:
		return "with " + countOf(name, shown(lo)), ""
	case kind == "len":
		return "with " + countOf(name, "up to "+shown(hi)), ""
	case kind == "num" && lo == top+1:
		return "with " + name + " above " + shown(top), ""
	case kind == "bytes" && lo == top+1:
		return "with " + name + " longer than " + shown(top) + " bytes", ""
	case lo > top:
		return "with " + name + of + shown(lo) + "+" + unit, ""
	case lo == hi && lo == 1:
		return "with " + name + of + "1" + strings.TrimSuffix(unit, "s"), ""
	case lo == hi:
		return "with " + name + of + shown(lo) + unit, ""
	}
	return "with " + name + of + "up to " + shown(hi) + unit, ""
}

func countOf(list, n string) string {
	one, plain := strings.TrimSuffix(list, "s"), !strings.ContainsAny(list, ".[")
	switch {
	case plain && one != list && n == "1":
		return "1 " + one
	case plain && one != list:
		return n + " " + list
	case n == "1":
		return list + " of 1 item"
	}
	return list + " of " + n + " items"
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
