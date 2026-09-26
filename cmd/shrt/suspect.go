package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func stepRefs(rec *runner.Record, at int) map[string]bool {
	out := map[string]bool{}
	for path, ref := range rec.Steps[at].BodyRefs {
		if diff.IDNamedPath(path) && !wholeRef.MatchString(strings.TrimSpace(ref)) {
			continue
		}
		for _, m := range bodyRefExpr.FindAllStringSubmatch(ref, -1) {
			r := chain.ParseRef(m[1])
			switch {
			case r.Kind == chain.RefStep && r.Rest != "":
				out[r.Head] = true
			case r.Kind == chain.RefBare, r.Kind == chain.RefExports:
				name := r.Head
				if r.Kind == chain.RefExports {
					name, _, _ = strings.Cut(r.Rest, ".")
				}
				if src := exporter(rec, at, name); src != "" {
					out[src] = true
				}
			}
		}
	}
	return out
}

func exporter(rec *runner.Record, at int, name string) string {
	for i := at - 1; i >= 0; i-- {
		if st := rec.Steps[i]; st != nil {
			if _, ok := st.Exported[name]; ok {
				return st.ID
			}
		}
	}
	return ""
}

var bodyRefExpr = regexp.MustCompile(`\$\{([^}]+)\}`)

var wholeRef = regexp.MustCompile(`^\$\{[^}]+\}$`)

func isWrite(st *runner.StepRecord) bool {
	return st != nil && !chain.IsReadOnlyCall(st.Call)
}

func suspectWrite(rec *runner.Record, step string, bad map[string]bool) (int, bool) {
	at, pos := -1, map[string]int{}
	for i, st := range rec.Steps {
		if st == nil {
			continue
		}
		if _, seen := pos[st.ID]; !seen {
			pos[st.ID] = i
		}
		if st.ID == step {
			at = i
		}
	}
	if at < 0 || isWrite(rec.Steps[at]) {
		return -1, false
	}
	if _, w, ok := strings.Cut(rec.Steps[at].ID, "_after_"); ok {
		if j, ok := pos[w]; ok && j < at && isWrite(rec.Steps[j]) {
			return j, false
		}
	}
	reach := refReach(rec, pos)
	entities := stepRefs(rec, at)
	nearest, nearestBad := -1, -1
	for i := at - 1; i >= 0 && len(entities) > 0; i-- {
		w := rec.Steps[i]
		if !isWrite(w) {
			continue
		}
		match := entities[w.ID]
		for e := range reach(i) {
			match = match || entities[e]
		}
		if !match {
			continue
		}
		if nearest < 0 {
			nearest = i
		}
		if bad[w.ID] && nearestBad < 0 {
			nearestBad = i
		}
	}
	switch {
	case nearestBad >= 0:
		return nearestBad, false
	case nearest >= 0:
		return nearest, false
	}
	for i := at - 1; i >= 0; i-- {
		if w := rec.Steps[i]; isWrite(w) && bad[w.ID] {
			return i, true
		}
	}
	return -1, false
}

func refReach(rec *runner.Record, pos map[string]int) func(int) map[string]bool {
	closure := map[int]map[string]bool{}
	var reach func(i int) map[string]bool
	reach = func(i int) map[string]bool {
		if c, ok := closure[i]; ok {
			return c
		}
		c := map[string]bool{}
		closure[i] = c
		for ref := range stepRefs(rec, i) {
			c[ref] = true
			if j, ok := pos[ref]; ok && j < i {
				for r := range reach(j) {
					c[r] = true
				}
			}
		}
		return c
	}
	return reach
}

type blame struct {
	write   int
	knock   bool
	own     string
	cascade string
	why     string
	firm    bool
}

type attribution struct {
	e         *env
	rec       *runner.Record
	bad       map[string]bool
	unchanged func(step, path string) bool
	reordered func(step, path string) bool
	changed   func(step string) []string
	resized   func(step, path string) string
	was       func(step, path string) (any, bool)
}

func (a attribution) of(step, path string) blame {
	b := blame{write: -1}
	st, ok := a.rec.Step(step)
	if !ok || st == nil {
		return b
	}
	if why := serverError(st); why != "" && !isWrite(st) {
		b.own = fmt.Sprintf("%s fails on its own (%s)", methodName(st.Call), why)
		return b
	}
	if src, ref := heldBackBy(st); src != "" && src != step {
		up := a.of(src, ref)
		if up.own != "" {
			b.own = up.own
			return b
		}
		b.write, b.cascade = up.write, "unevaluated because "+a.lost(src, ref)
		if b.write < 0 {
			b.write = a.index(src)
		}
		return b
	}
	if w, why := a.afterFailedWrite(step, path); w >= 0 {
		return blame{write: w, cascade: why}
	}
	if why := a.refused(st); why != "" && !isWrite(st) && !a.writeRefusedBefore(step) {
		b.own = why
		return b
	}
	if e, p := a.earlier(step, path); e >= 0 {
		if isWrite(a.rec.Steps[e]) {
			if w := a.recomputed(a.rec.Steps[e].ID, p); w >= 0 {
				return blame{write: w}
			}
			return blame{write: e}
		}
		return a.of(a.rec.Steps[e].ID, p)
	}
	if w := a.recomputed(step, path); w >= 0 {
		return blame{write: w}
	}
	if isWrite(st) {
		return b
	}
	if list := ""; a.resized != nil && path != "" && !a.writeRefusedBefore(step) {
		if list = a.resized(step, path); list != "" {
			b.own = fmt.Sprintf("%s answers another set of %s", methodName(st.Call), list)
			if !a.writeChangedBefore(step) {
				b.own += ", and the writes before it answered as before"
			}
			return b
		}
	}
	if a.reordered != nil && path != "" && a.reordered(step, path) {
		b.own = fmt.Sprintf("%s answers the same items in another order", methodName(st.Call))
		return b
	}
	b.write, b.knock = suspectWrite(a.rec, step, a.bad)
	if b.knock && !a.explains(b.write, st, path) {
		b.write, b.knock = -1, false
	}
	if b.write >= 0 && !b.knock {
		w := a.rec.Steps[b.write]
		if t, ok := a.echoed(b.write, st, path); ok {
			return t
		}
		var changed []string
		if a.changed != nil {
			changed = a.changed(w.ID)
		}
		for _, p := range changed {
			if up, why := a.afterFailedWrite(w.ID, p); up >= 0 {
				return blame{write: up, cascade: why}
			}
		}
	}
	return b
}

func (a attribution) index(step string) int {
	for i, st := range a.rec.Steps {
		if st != nil && st.ID == step {
			return i
		}
	}
	return -1
}

func (a attribution) writeChangedBefore(step string) bool {
	for _, st := range a.rec.Steps {
		if st == nil || st.ID == step {
			return false
		}
		if isWrite(st) && a.bad[st.ID] {
			return true
		}
	}
	return false
}

func (a attribution) writeRefusedBefore(step string) bool {
	for _, st := range a.rec.Steps {
		if st == nil || st.ID == step {
			return false
		}
		if isWrite(st) && a.bad[st.ID] && (a.flipped(st) != "" || st.Transport != nil || a.changed == nil || len(a.changed(st.ID)) == 0) {
			return true
		}
	}
	return false
}

func (a attribution) flipped(st *runner.StepRecord) string {
	why := refusalOf(st)
	if why == "" || !a.bad[st.ID] {
		return ""
	}
	if st.Transport != nil {
		return why
	}
	if a.was != nil {
		if _, ok := a.was(st.ID, chain.EnvelopePath()); ok {
			return why
		}
	}
	return ""
}

func refusalOf(st *runner.StepRecord) string {
	if st.Transport != nil {
		return st.Transport.Code
	}
	v := verdictOf(st)
	if v.ErrorCode == "" || v.ErrorCode == chain.EnvelopeOK() {
		return ""
	}
	parts := []string{}
	for _, f := range []string{"app_code", "reason"} {
		if s := v.Refusal[f]; s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return v.ErrorCode
	}
	return strings.Join(parts, " ")
}

func (a attribution) refused(st *runner.StepRecord) string {
	why := a.flipped(st)
	if why == "" {
		return ""
	}
	for _, o := range a.rec.Steps {
		if o == nil || o == st || o.Call != st.Call || profileOf(o) == profileOf(st) || o.Status == runner.StatusSkipped || len(o.Response) == 0 || refusalOf(o) != "" {
			continue
		}
		return fmt.Sprintf("%s passes as %s, refused as %s (%s)", methodName(st.Call), profileOf(o), profileOf(st), why)
	}
	return fmt.Sprintf("%s is refused (%s)", methodName(st.Call), why)
}

func profileOf(st *runner.StepRecord) string {
	if st.AuthProfile == "" {
		return "default"
	}
	return st.AuthProfile
}

func (a attribution) explains(wi int, st *runner.StepRecord, path string) bool {
	if wi < 0 || a.changed == nil || path == "" {
		return false
	}
	w := a.rec.Steps[wi]
	var rb, wb any
	if json.Unmarshal(st.Response, &rb) != nil || json.Unmarshal(w.Response, &wb) != nil {
		return false
	}
	rv, ok := chain.Get(rb, path)
	if !ok {
		return false
	}
	for _, p := range a.changed(w.ID) {
		if v, ok := chain.Get(wb, p); ok && compactValue(v) == compactValue(rv) {
			return true
		}
	}
	return false
}

func number(v any) (float64, bool) {
	switch v.(type) {
	case nil, bool, map[string]any, []any:
		return 0, false
	}
	n, err := strconv.ParseFloat(fmt.Sprint(v), 64)
	return n, err == nil
}

type input struct {
	was, now float64
	at       int
}

func (a attribution) recomputed(step, path string) int {
	at := a.index(step)
	if at < 0 || a.was == nil || path == "" {
		return -1
	}
	var body any
	if json.Unmarshal(a.rec.Steps[at].Response, &body) != nil {
		return -1
	}
	now, _ := chain.Get(body, path)
	old, _ := a.was(step, path)
	nv, okNow := number(now)
	wv, okWas := number(old)
	if !okNow || !okWas || nv == wv {
		return -1
	}
	segs := chain.SplitPath(path)
	holder := body
	if len(segs) > 1 {
		holder, _ = chain.Get(body, strings.Join(segs[:len(segs)-1], "."))
	}
	obj, ok := holder.(map[string]any)
	if !ok {
		return -1
	}
	inputs := a.inputsBefore(at)
	for _, v := range obj {
		lines, ok := v.([]any)
		if !ok || len(lines) == 0 {
			continue
		}
		for _, known := range inputs {
			if w := linesSum(lines, known, nv, wv); w >= 0 {
				return w
			}
		}
	}
	return -1
}

func (a attribution) inputs(step, path string) string {
	at := a.index(step)
	if at < 0 || a.was == nil || path == "" {
		return ""
	}
	var body any
	if json.Unmarshal(a.rec.Steps[at].Response, &body) != nil {
		return ""
	}
	now, _ := chain.Get(body, path)
	old, _ := a.was(step, path)
	nv, okNow := number(now)
	wv, okWas := number(old)
	segs := chain.SplitPath(path)
	if !okNow || !okWas || nv == wv || len(segs) < 2 {
		return ""
	}
	holder, _ := chain.Get(body, strings.Join(segs[:len(segs)-1], "."))
	obj, ok := holder.(map[string]any)
	if !ok {
		return ""
	}
	known := a.inputsBefore(at)
	for _, key := range sortedKeys(obj) {
		lines, ok := obj[key].([]any)
		if !ok || len(lines) == 0 {
			continue
		}
		for _, leaf := range sortedKeys(known) {
			if terms := linesTerms(lines, known[leaf], wv); terms != nil {
				return key + ": " + strings.Join(terms, ", ")
			}
		}
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func linesTerms(lines []any, known map[string]input, want float64) []string {
	first, _ := lines[0].(map[string]any)
	factors := []string{""}
	for _, k := range sortedKeys(first) {
		if _, ok := number(first[k]); ok {
			factors = append(factors, k)
		}
	}
	for _, f := range factors {
		sum, terms := 0.0, []string{}
		for _, l := range lines {
			m, _ := l.(map[string]any)
			in, found := input{}, false
			for _, x := range m {
				if s, ok := x.(string); ok {
					if got, ok := known[s]; ok {
						in, found = got, true
					}
				}
			}
			q, ok := 1.0, true
			if f != "" {
				q, ok = number(m[f])
			}
			if !found || !ok {
				terms = nil
				break
			}
			sum += q * in.was
			term := strconv.FormatFloat(in.now, 'f', -1, 64)
			if f != "" {
				term = strconv.FormatFloat(q, 'f', -1, 64) + " x " + term
			}
			terms = append(terms, term)
		}
		if terms != nil && sum == want {
			return terms
		}
	}
	return nil
}

func (a attribution) inputsBefore(at int) map[string]map[string]input {
	inputs := map[string]map[string]input{}
	for i := 0; i < at; i++ {
		prev := a.rec.Steps[i]
		var pb any
		if prev == nil || json.Unmarshal(prev.Response, &pb) != nil {
			continue
		}
		eachLeaf(pb, "", func(p string, v any) {
			n, ok := number(v)
			ps := chain.SplitPath(p)
			if !ok || len(ps) < 2 {
				return
			}
			leaf := ps[len(ps)-1]
			parent, _ := chain.Get(pb, strings.Join(ps[:len(ps)-1], "."))
			for _, id := range idsOf(parent) {
				in := input{was: n, now: n, at: -1}
				if old, ok := a.was(prev.ID, p); ok {
					if o, ok := number(old); ok && o != n {
						in.was, in.at = o, i
					}
				}
				if inputs[leaf] == nil {
					inputs[leaf] = map[string]input{}
				}
				if cur, seen := inputs[leaf][id]; seen && cur.at >= 0 {
					in.was, in.at = cur.was, cur.at
				}
				inputs[leaf][id] = in
			}
		})
	}
	return inputs
}

func idsOf(v any) []string {
	m, _ := v.(map[string]any)
	var out []string
	for k, x := range m {
		lower := strings.ToLower(k)
		if s, ok := x.(string); ok && s != "" && (lower == "id" || strings.HasPrefix(lower, "id_") || strings.HasSuffix(lower, "_id")) {
			out = append(out, s)
		}
	}
	return out
}

func linesSum(lines []any, known map[string]input, nv, wv float64) int {
	first, _ := lines[0].(map[string]any)
	factors := []string{""}
	for k, x := range first {
		if _, ok := number(x); ok {
			factors = append(factors, k)
		}
	}
	for _, f := range factors {
		sumNow, sumWas, from := 0.0, 0.0, -1
		for _, l := range lines {
			m, _ := l.(map[string]any)
			in, found := input{}, false
			for _, x := range m {
				if s, ok := x.(string); ok {
					if got, ok := known[s]; ok {
						in, found = got, true
					}
				}
			}
			q, ok := 1.0, true
			if f != "" {
				q, ok = number(m[f])
			}
			if !found || !ok {
				from = -2
				break
			}
			sumNow += q * in.now
			sumWas += q * in.was
			if in.at >= 0 && (from < 0 || in.at < from) {
				from = in.at
			}
		}
		if from >= 0 && sumNow == nv && sumWas == wv {
			return from
		}
	}
	return -1
}

func (a attribution) lost(src, path string) string {
	st, ok := a.rec.Step(src)
	if !ok || st == nil {
		return ""
	}
	if path == "" {
		return methodName(st.Call) + " failed"
	}
	var body any
	if json.Unmarshal(st.Response, &body) == nil {
		if v, ok := chain.Get(body, path); ok && v != nil {
			return methodName(st.Call) + " changed " + path
		}
	}
	return methodName(st.Call) + " lost " + path
}

func (a attribution) afterFailedWrite(step, path string) (int, string) {
	at := a.index(step)
	if at < 0 || path == "" || a.e == nil || a.e.cat == nil {
		return -1, ""
	}
	leaf := ""
	for _, seg := range chain.SplitPath(path) {
		if _, err := strconv.Atoi(seg); err != nil {
			leaf = seg
		}
	}
	pos := map[string]int{}
	for i, st := range a.rec.Steps {
		if _, seen := pos[st.ID]; st != nil && !seen {
			pos[st.ID] = i
		}
	}
	touched := refReach(a.rec, pos)(at)
	for i, w := range a.rec.Steps[:at] {
		if !isWrite(w) || !a.bad[w.ID] || w.Transport == nil || (w.Status != runner.StatusFailed && w.Status != runner.StatusError) {
			continue
		}
		same := false
		for ref := range stepRefs(a.rec, i) {
			same = same || touched[ref]
		}
		if m, err := a.e.cat.Lookup(w.Call); same && err == nil && declares(m.Output(), leaf, 0) {
			return i, fmt.Sprintf("after %s failed on the same record", methodName(w.Call))
		}
	}
	return -1, ""
}

func declares(md protoreflect.MessageDescriptor, name string, depth int) bool {
	if md == nil || depth > 4 {
		return false
	}
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if string(fd.Name()) == name || fd.JSONName() == name || declares(fd.Message(), name, depth+1) {
			return true
		}
	}
	return false
}

func (a attribution) earlier(step, path string) (int, string) {
	at := a.index(step)
	if a.e == nil || a.e.cat == nil || a.changed == nil || path == "" || at < 0 {
		return -1, ""
	}
	st := a.rec.Steps[at]
	m, err := a.e.cat.Lookup(st.Call)
	if err != nil {
		return -1, ""
	}
	want, ok := carrierOf(m, path)
	if !ok {
		return -1, ""
	}
	var body any
	_ = json.Unmarshal(st.Response, &body)
	pos := map[string]int{}
	for i, s := range a.rec.Steps {
		if _, seen := pos[s.ID]; s != nil && !seen {
			pos[s.ID] = i
		}
	}
	reach := refReach(a.rec, pos)
	for i := 0; i < at; i++ {
		w := a.rec.Steps[i]
		if w == nil || isWrite(st) && isWrite(w) && !related(reach, at, i, w.ID) {
			continue
		}
		wm, err := a.e.cat.Lookup(w.Call)
		if err != nil {
			continue
		}
		var wb any
		_ = json.Unmarshal(w.Response, &wb)
		for _, p := range a.changed(w.ID) {
			if c, ok := carrierOf(wm, p); ok && c == want && sameEntity(body, path, wb, p) {
				return i, p
			}
		}
	}
	return -1, ""
}

func related(reach func(int) map[string]bool, at, i int, id string) bool {
	touched := reach(at)
	if touched[id] {
		return true
	}
	for ref := range reach(i) {
		if touched[ref] {
			return true
		}
	}
	return false
}

func heldBackBy(st *runner.StepRecord) (string, string) {
	src, path := "", ""
	for _, ex := range st.Expect {
		if ex.Passed {
			continue
		}
		_, rest, ok := strings.Cut(ex.Detail, `reads step "`)
		if ex.Rule != "unevaluated" || !ok {
			return "", ""
		}
		if name, _, _ := strings.Cut(rest, `"`); src == "" {
			src = name
			_, ref, _ := strings.Cut(ex.Detail, "${")
			ref, _, _ = strings.Cut(ref, "}")
			if r := chain.ParseRef(ref); r.Kind == chain.RefStep && r.Head == src {
				path = strings.TrimPrefix(r.Rest, "response.")
			}
		}
	}
	return src, path
}

func (a attribution) echoed(wi int, r *runner.StepRecord, path string) (blame, bool) {
	w := a.rec.Steps[wi]
	if a.e == nil || a.e.cat == nil || a.unchanged == nil || path == "" {
		return blame{}, false
	}
	rm, err := a.e.cat.Lookup(r.Call)
	if err != nil {
		return blame{}, false
	}
	wm, err := a.e.cat.Lookup(w.Call)
	if err != nil {
		return blame{}, false
	}
	want, ok := carrierOf(rm, path)
	if !ok {
		return blame{}, false
	}
	var rb, wb any
	if json.Unmarshal(r.Response, &rb) != nil || json.Unmarshal(w.Response, &wb) != nil {
		return blame{}, false
	}
	rv, ok := chain.Get(rb, path)
	if !ok {
		return blame{}, false
	}
	wp, wv, found := "", "", false
	eachLeaf(wb, "", func(p string, v any) {
		if found {
			return
		}
		if c, ok := carrierOf(wm, p); !ok || c != want {
			return
		}
		if sameEntity(rb, path, wb, p) && a.unchanged(w.ID, p) && compactValue(v) != compactValue(rv) {
			wp, wv, found = p, compactValue(v), true
		}
	})
	if !found {
		return blame{}, false
	}
	shown := gateIndex.ReplaceAllString(path, "[]$1")
	agree, confirm := []string{methodName(r.Call)}, false
	for _, o := range a.rec.Steps[wi+1:] {
		if o == nil || o == r || isWrite(o) {
			continue
		}
		om, err := a.e.cat.Lookup(o.Call)
		var ob any
		if err != nil || json.Unmarshal(o.Response, &ob) != nil {
			continue
		}
		eachLeaf(ob, "", func(p string, v any) {
			if c, ok := carrierOf(om, p); !ok || c != want || !sameEntity(wb, wp, ob, p) {
				return
			}
			switch compactValue(v) {
			case wv:
				confirm = true
			case compactValue(rv):
				if !containsName(agree, methodName(o.Call)) {
					agree = append(agree, methodName(o.Call))
				}
			}
		})
	}
	switch {
	case confirm:
		return blame{write: -1, own: fmt.Sprintf("%s answers %s differently from what %s returned for the same record",
			methodName(r.Call), shown, methodName(w.Call))}, true
	case len(agree) > 1:
		return blame{write: wi, firm: true, why: fmt.Sprintf("%s answered %s %s, but %s read %s: it did not store what it answered",
			methodName(w.Call), shown, capText(wv, 60), strings.Join(agree, ", "), capText(compactValue(rv), 60))}, true
	}
	return blame{write: wi, why: fmt.Sprintf("%s answered %s %s, %s reads %s: the write stored something else or the read changes it",
		methodName(w.Call), shown, capText(wv, 60), agree[0], capText(compactValue(rv), 60))}, true
}

func carrierOf(m *catalog.Method, path string) (string, bool) {
	md := m.Output()
	segs := chain.SplitPath(path)
	if m.ServerStreaming && len(segs) > 1 && segs[0] == catalog.StreamMessages {
		if _, err := strconv.Atoi(segs[1]); err == nil {
			segs = segs[2:]
		}
	}
	for len(segs) > 0 {
		if _, err := strconv.Atoi(segs[len(segs)-1]); err != nil {
			break
		}
		segs = segs[:len(segs)-1]
	}
	skip := false
	for i, seg := range segs {
		if skip {
			skip = false
			continue
		}
		if _, err := strconv.Atoi(seg); err == nil {
			continue
		}
		if md == nil {
			return "", false
		}
		fd := md.Fields().ByName(protoreflect.Name(seg))
		if fd == nil {
			fd = md.Fields().ByJSONName(seg)
		}
		if fd == nil {
			return "", false
		}
		if i == len(segs)-1 {
			return string(md.FullName()) + "." + string(fd.Name()), true
		}
		if fd.IsMap() {
			skip = true
			md = fd.MapValue().Message()
			continue
		}
		md = fd.Message()
	}
	return "", false
}

func eachLeaf(v any, path string, visit func(string, any)) {
	join := func(k string) string {
		if path == "" {
			return k
		}
		return path + "." + k
	}
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			eachLeaf(x, join(k), visit)
		}
	case []any:
		for i, x := range t {
			eachLeaf(x, join(strconv.Itoa(i)), visit)
		}
	default:
		visit(path, v)
	}
}

func sameEntity(a any, pa string, b any, pb string) bool {
	parent := func(root any, path string) map[string]any {
		segs := chain.SplitPath(path)
		if len(segs) < 2 {
			return nil
		}
		v, _ := chain.Get(root, strings.Join(segs[:len(segs)-1], "."))
		m, _ := v.(map[string]any)
		return m
	}
	x, y := parent(a, pa), parent(b, pb)
	sawID := false
	for k, v := range x {
		lower := strings.ToLower(k)
		if lower != "id" && !strings.HasPrefix(lower, "id_") && !strings.HasSuffix(lower, "_id") {
			continue
		}
		if w, ok := y[k]; ok {
			if compactValue(v) == compactValue(w) {
				return true
			}
			sawID = true
		}
	}
	return !sawID
}

func requestLine(rec *runner.Record, step string, b blame) string {
	st, ok := rec.Step(step)
	if !ok || st == nil {
		return ""
	}
	lead := step
	if b.own != "" {
		lead = fmt.Sprintf("suspect read %s (%s)", step, shortRPC(st.Call))
	}
	if b.write >= 0 {
		st = rec.Steps[b.write]
		lead = fmt.Sprintf("suspect write %s (%s)", st.ID, shortRPC(st.Call))
	}
	if sent := sentText(st); sent != "" {
		return lead + sent
	}
	return ""
}

func sentText(st *runner.StepRecord) string {
	var buf bytes.Buffer
	if len(st.Request) == 0 || json.Compact(&buf, st.Request) != nil {
		return ""
	}
	as := ""
	if st.AuthProfile != "" && st.AuthProfile != "default" {
		as = " as " + st.AuthProfile
	}
	return as + " sent " + capText(buf.String(), 300)
}
