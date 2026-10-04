package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
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

func suspectWrite(rec *runner.Record, step, path string, bad map[string]bool, from int) (int, bool) {
	at := -1
	for i, st := range rec.Steps {
		if st != nil && st.ID == step {
			at = i
		}
	}
	if at < 0 || isWrite(rec.Steps[at]) {
		return -1, false
	}
	nearest, nearestBad := -1, -1
	for _, i := range entityWrites(rec, at, path, bad, from) {
		if nearest < 0 {
			nearest = i
		}
		if bad[rec.Steps[i].ID] && nearestBad < 0 {
			nearestBad = i
		}
	}
	switch {
	case nearestBad >= 0:
		return nearestBad, false
	case nearest >= 0:
		return nearest, false
	}
	for i := at - 1; i > from; i-- {
		if w := rec.Steps[i]; isWrite(w) && bad[w.ID] {
			return i, true
		}
	}
	return -1, false
}

func entityWrites(rec *runner.Record, at int, path string, bad map[string]bool, from int) []int {
	reach := refReach(rec)
	entities := stepRefs(rec, at)
	for _, id := range itemIDs(rec.Steps[at], path) {
		if src := producer(rec, at, id); src != "" {
			entities[src] = true
		}
	}
	var out []int
	for i := at - 1; i > from && len(entities) > 0; i-- {
		w := rec.Steps[i]
		if !isWrite(w) || inert(rec, i, bad) {
			continue
		}
		match := entities[w.ID]
		for e := range reach(i) {
			match = match || entities[e]
		}
		if match {
			out = append(out, i)
		}
	}
	return out
}

func itemIDs(st *runner.StepRecord, path string) []string {
	segs := chain.SplitPath(path)
	var body any
	if st == nil || json.Unmarshal(st.Response, &body) != nil {
		return nil
	}
	for k := len(segs) - 1; k > 0; k-- {
		if _, err := strconv.Atoi(segs[k]); err == nil {
			item, _ := chain.Get(body, strings.Join(segs[:k+1], "."))
			return idsOf(item)
		}
	}
	return nil
}

func producer(rec *runner.Record, at int, id string) string {
	for _, st := range rec.Steps[:at] {
		var body any
		if st == nil || json.Unmarshal(st.Response, &body) != nil {
			continue
		}
		found := false
		eachLeaf(body, "", func(p string, v any) {
			segs := chain.SplitPath(p)
			found = found || v == id && diff.IDNamedPath(segs[len(segs)-1])
		})
		if found {
			return st.ID
		}
	}
	return ""
}

func inert(rec *runner.Record, i int, bad map[string]bool) bool {
	w := rec.Steps[i]
	if bad[w.ID] {
		return false
	}
	if refusalOf(w) != "" {
		refs := stepRefs(rec, i)
		for j, o := range rec.Steps[:i] {
			if o == nil || o.Call != w.Call || refusalOf(o) != "" {
				continue
			}
			if overlaps(stepRefs(rec, j), refs) {
				return true
			}
		}
	}
	for _, ref := range w.BodyRefs {
		m := bodyRefExpr.FindStringSubmatch(ref)
		if m == nil || !wholeRef.MatchString(strings.TrimSpace(ref)) {
			continue
		}
		r := chain.ParseRef(m[1])
		if r.Kind != chain.RefStep || !strings.HasPrefix(r.Rest, "request.") {
			continue
		}
		for _, o := range rec.Steps[:i] {
			if o != nil && o.ID == r.Head && o.Call == w.Call && sameIDs(o.Response, w.Response) {
				return true
			}
		}
	}
	return false
}

func sameIDs(a, b json.RawMessage) bool {
	var x, y map[string]any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	found := false
	for k, v := range x {
		ids, other := idsOf(v), idsOf(y[k])
		sort.Strings(ids)
		sort.Strings(other)
		if len(ids) == 0 {
			continue
		}
		if strings.Join(ids, " ") != strings.Join(other, " ") {
			return false
		}
		found = true
	}
	return found
}

func refReach(rec *runner.Record) func(int) map[string]bool {
	pos := positions(rec)
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

type attribution struct {
	e         *env
	ref       bool
	rec       *runner.Record
	bad       map[string]bool
	unchanged func(step, path string) bool
	reordered func(step, path string) bool
	changed   func(step string) []string
	resized   func(step, path string) string
	was       func(step, path string) (any, bool)
}

func (a attribution) own(kind string, st *runner.StepRecord) reason {
	return reason{Kind: kind, Step: st.ID, RPC: st.Call, Profile: profileAs(a.e, st)}
}

func (a attribution) write(i int) reason {
	if i < 0 {
		return reason{}
	}
	return a.own(reasonWrite, a.rec.Steps[i])
}

func (a attribution) knockOn(i int) reason {
	r := a.write(i)
	r.Kind = reasonKnockOn
	return r
}

func (a attribution) of(step, path string) reason {
	st, ok := a.rec.Step(step)
	if !ok || st == nil {
		return reason{}
	}
	if why := serverError(st); why != "" && !isWrite(st) {
		r := a.own(reasonError, st)
		r.Got = why
		return r
	}
	if p := profileOf(st); (p == runner.NoAuthProfile || p == chain.InvalidTokenAuth) && chain.IsTransportPath(path) {
		r := a.own(reasonProbe, st)
		r.Path = path
		return r
	}
	if other := a.heldApart(st, path); other != "" {
		return a.profiled(st, path, other)
	}
	if src, ref := heldBackBy(st); src != "" && src != step && (a.changed == nil || !slices.Contains(a.changed(step), path)) {
		up, i := a.of(src, ref), -1
		if up.blames() {
			i = a.index(up.blamed(src))
			if j := a.index(src); i < 0 && j >= 0 && isWrite(a.rec.Steps[j]) {
				i = j
			}
		}
		if i >= 0 {
			return a.knockOn(i)
		}
		s, _ := a.rec.Step(src)
		return reason{Kind: reasonKnockOn, Read: src, ReadRPC: s.Call}
	}
	if w := a.afterFailedWrite(step, path); w >= 0 {
		return a.knockOn(w)
	}
	if code, other := a.refused(st); code != "" && !isWrite(st) && !a.writeRefusedBefore(step) {
		r := a.own(reasonRefused, st)
		r.Got, r.Other = code, other
		return r
	}
	if e, p := a.earlier(step, path); e >= 0 {
		if isWrite(a.rec.Steps[e]) {
			if w := a.recomputed(a.rec.Steps[e].ID, p); w >= 0 {
				return a.write(w)
			}
			return a.write(a.behind(e))
		}
		return a.of(a.rec.Steps[e].ID, p)
	}
	if w := a.recomputed(step, path); w >= 0 {
		return a.write(w)
	}
	if isWrite(st) {
		stored, w := a.unstored(st, path), -1
		if a.flipped(st) != "" {
			w = a.changedWriteBefore(step)
		} else if stored.Kind == "" {
			w = a.sameRecordWriteBefore(step)
		}
		if w < 0 && stored.Kind == "" {
			w = a.upstream(step)
		}
		switch {
		case w >= 0:
			return a.write(w)
		case stored.Kind != "":
			return stored
		}
		return a.own(reasonWrite, st)
	}
	if was, ok := a.accepted(st); ok && !a.writeChangedBefore(step) {
		r := a.own(reasonCode, st)
		r.Want, r.Got = fmt.Sprint(was), verdictOf(st).ErrorCode
		return r
	}
	if a.resized != nil && path != "" && !a.writeRefusedBefore(step) {
		if list := a.resized(step, path); list != "" {
			if w := a.listedFrom(step, path); w >= 0 {
				return a.write(w)
			}
			r := a.own(reasonSet, st)
			r.Path = list
			return r
		}
	}
	if a.reordered != nil && path != "" && a.reordered(step, path) {
		r := a.own(reasonOrder, st)
		r.Other = a.orderKey(st, path)
		return r
	}
	if other := a.principal(st, path); other != "" {
		return a.profiled(st, path, other)
	}
	from, p := a.lastMatch(st, path)
	if from >= 0 && !a.unchanged(a.rec.Steps[from].ID, p) {
		return a.of(a.rec.Steps[from].ID, p)
	}
	w, knock := suspectWrite(a.rec, step, path, a.movedFor(path), from)
	switch {
	case knock && a.explains(w, st, path):
		return a.knockOn(w)
	case knock:
		return reason{}
	}
	if w >= 0 && !a.bears(w, path) {
		w = a.bearing(a.index(step), path)
	}
	if w < 0 {
		return reason{}
	}
	if r, ok := a.echoed(w, st, path); ok {
		return r
	}
	if a.changed != nil {
		for _, p := range a.changed(a.rec.Steps[w].ID) {
			if up := a.afterFailedWrite(a.rec.Steps[w].ID, p); up >= 0 {
				return a.knockOn(up)
			}
		}
	}
	if up := a.behind(w); up != w {
		return a.write(up)
	}
	if !a.movedFor(path)[a.rec.Steps[w].ID] {
		return a.asBefore(a.index(step), path, w)
	}
	return a.write(w)
}

func (a attribution) profiled(st *runner.StepRecord, path, other string) reason {
	r := a.own(reasonProfile, st)
	r.Profile, r.Path, r.Other = profileOf(st), path, other
	return r
}

func (a attribution) asBefore(at int, path string, w int) reason {
	st := a.rec.Steps[at]
	from, _ := a.lastMatch(st, path)
	var ws []reason
	for _, i := range entityWrites(a.rec, at, path, a.bad, from) {
		if a.answers(a.rec.Steps[i], st, path) {
			break
		}
		if a.moves(i, path) {
			ws = append([]reason{{Step: a.rec.Steps[i].ID, RPC: a.rec.Steps[i].Call, Profile: profileAs(a.e, a.rec.Steps[i])}}, ws...)
		}
	}
	if len(ws) < 2 {
		return a.write(w)
	}
	r := a.write(a.index(ws[0].Step))
	r.Kind, r.Or = reasonUnclear, ws
	return r
}

func (a attribution) moves(i int, path string) bool {
	w := a.rec.Steps[i]
	eff := a.e.effectsOf(w.Call)[leafOf(path)]
	switch {
	case eff != nil && eff.Restore != "" && !a.seenIn(i, eff.Restore):
		return false
	case a.ref:
		return a.bears(i, path)
	}
	return eff != nil && eff.Is != contract.EffectNone && refusalOf(w) == ""
}

func (a attribution) seenIn(i int, state string) bool {
	ids, found := idsOf(decoded(a.rec.Steps[i].Request)), false
	for _, st := range a.rec.Steps[:i] {
		var body any
		if st == nil || json.Unmarshal(st.Response, &body) != nil {
			continue
		}
		eachLeaf(body, "", func(p string, v any) {
			if s, ok := v.(string); !found && ok && contract.SameState(s, state) {
				segs := chain.SplitPath(p)
				holder, _ := chain.Get(body, strings.Join(segs[:len(segs)-1], "."))
				found = slices.ContainsFunc(idsOf(holder), func(id string) bool { return slices.Contains(ids, id) })
			}
		})
	}
	return found
}

func (a attribution) answers(w, st *runner.StepRecord, path string) bool {
	var rb, wb any
	if a.unchanged == nil || path == "" || envelopeOnly(path) || json.Unmarshal(st.Response, &rb) != nil || json.Unmarshal(w.Response, &wb) != nil {
		return false
	}
	found := false
	eachLeaf(wb, "", func(p string, _ any) {
		found = found || leafOf(p) == leafOf(path) && sameEntity(rb, path, wb, p) && a.unchanged(w.ID, p)
	})
	return found
}

func (a attribution) movedFor(path string) map[string]bool {
	out := map[string]bool{}
	for id := range a.bad {
		st, _ := a.rec.Step(id)
		var ch []string
		if a.changed != nil {
			ch = a.changed(id)
		}
		out[id] = path == "" || !isWrite(st) || len(ch) == 0 || verdictMoved(ch) || slices.ContainsFunc(ch, func(p string) bool {
			return slices.Contains(chain.SplitPath(p), leafOf(path)) || slices.Contains(chain.SplitPath(path), leafOf(p))
		})
	}
	return out
}

func (a attribution) lastMatch(st *runner.StepRecord, path string) (int, string) {
	var rb any
	if a.unchanged == nil || a.was == nil || path == "" || envelopeOnly(path) || json.Unmarshal(st.Response, &rb) != nil {
		return -1, ""
	}
	now, _ := chain.Get(rb, path)
	was, _ := a.was(st.ID, path)
	for i := a.index(st.ID) - 1; i >= 0; i-- {
		o, found := a.rec.Steps[i], ""
		var ob any
		if o == nil || isWrite(o) || refusalOf(o) != "" || json.Unmarshal(o.Response, &ob) != nil {
			continue
		}
		eachLeaf(ob, "", func(p string, v any) {
			if old, ok := a.was(o.ID, p); found == "" && leafOf(p) == leafOf(path) && sameEntity(rb, path, ob, p) && (a.unchanged(o.ID, p) || ok && compactValue(old) == compactValue(was) && compactValue(v) == compactValue(now)) {
				found = p
			}
		})
		if found != "" {
			return i, found
		}
	}
	return -1, ""
}

func positions(rec *runner.Record) map[string]int {
	pos := map[string]int{}
	for i, st := range rec.Steps {
		if st == nil {
			continue
		}
		if _, seen := pos[st.ID]; !seen {
			pos[st.ID] = i
		}
	}
	return pos
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
	at := a.index(step)
	reach := refReach(a.rec)
	for i, st := range a.rec.Steps {
		if st == nil || st.ID == step {
			return false
		}
		if isWrite(st) && a.bad[st.ID] && related(reach, at, i, st.ID) && (a.flipped(st) != "" || st.Transport != nil || a.changed == nil || len(a.changed(st.ID)) == 0) {
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

func (a attribution) accepted(st *runner.StepRecord) (any, bool) {
	if a.was == nil || refusalOf(st) != "" || verdictOf(st).ErrorCode == "" {
		return nil, false
	}
	return a.was(st.ID, chain.EnvelopePath())
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

func (a attribution) refused(st *runner.StepRecord) (string, string) {
	why := a.flipped(st)
	if why == "" {
		return "", ""
	}
	for _, o := range a.rec.Steps {
		if o == nil || o == st || o.Call != st.Call || profileOf(o) == profileOf(st) || o.Status == runner.StatusSkipped || len(o.Response) == 0 || refusalOf(o) != "" {
			continue
		}
		return why, profileOf(o)
	}
	return why, ""
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
	return slices.ContainsFunc(a.changed(w.ID), func(p string) bool {
		v, ok := chain.Get(wb, p)
		return ok && compactValue(v) == compactValue(rv)
	})
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

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func keysOfBoth[V any](a, b map[string]V) []string {
	keys := append(slices.Collect(maps.Keys(a)), slices.Collect(maps.Keys(b))...)
	slices.Sort(keys)
	return slices.Compact(keys)
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

func (a attribution) afterFailedWrite(step, path string) int {
	at := a.index(step)
	if at < 0 || path == "" || a.e == nil || a.e.cat == nil {
		return -1
	}
	leaf := ""
	for _, seg := range chain.SplitPath(path) {
		if _, err := strconv.Atoi(seg); err != nil {
			leaf = seg
		}
	}
	touched := refReach(a.rec)(at)
	for i, w := range a.rec.Steps[:at] {
		if !isWrite(w) || !a.bad[w.ID] || w.Transport == nil || !failing(w) {
			continue
		}
		same := false
		for ref := range stepRefs(a.rec, i) {
			same = same || touched[ref]
		}
		if m, err := a.e.cat.Lookup(w.Call); same && err == nil && declares(m.Output(), leaf, 0) {
			return i
		}
	}
	return -1
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
	body := decoded(st.Response)
	reach := refReach(a.rec)
	for i := 0; i < at; i++ {
		w := a.rec.Steps[i]
		if w == nil || isWrite(st) && isWrite(w) && !related(reach, at, i, w.ID) {
			continue
		}
		wm, err := a.e.cat.Lookup(w.Call)
		if err != nil {
			continue
		}
		wb := decoded(w.Response)
		for _, p := range a.changed(w.ID) {
			c, ok := carrierOf(wm, p)
			if !ok || !sameEntity(body, path, wb, p) {
				continue
			}
			if c == want || isWrite(w) && leafName(c) == leafName(want) && related(reach, at, i, w.ID) {
				return i, p
			}
		}
	}
	return -1, ""
}

func (a attribution) behind(w int) int {
	if a.flipped(a.rec.Steps[w]) != "" {
		if up := a.changedWriteBefore(a.rec.Steps[w].ID); up >= 0 {
			return up
		}
	}
	if up := a.sameRecordWriteBefore(a.rec.Steps[w].ID); up >= 0 {
		return up
	}
	if up := a.upstream(a.rec.Steps[w].ID); a.bad[a.rec.Steps[w].ID] && up >= 0 {
		return up
	}
	return w
}

func (a attribution) upstream(step string) int {
	at := a.index(step)
	if at < 0 || a.changed == nil {
		return -1
	}
	reach := refReach(a.rec)
	uses := func(call, path string) bool {
		eff := a.e.effectsOf(call)[leafOf(path)]
		return eff != nil && eff.Is != contract.EffectNone
	}
	mine := a.changed(step)
	reached := func(p string) bool {
		return verdictMoved(mine) || leafFields(mine)[leafOf(p)]
	}
	for i := 0; i < at; i++ {
		o := a.rec.Steps[i]
		if o == nil || !a.bad[o.ID] || !related(reach, at, i, o.ID) {
			continue
		}
		if isWrite(o) {
			if verdictMoved(a.changed(o.ID)) {
				return a.behind(i)
			}
			continue
		}
		for _, p := range a.changed(o.ID) {
			if !uses(a.rec.Steps[at].Call, p) || !reached(p) {
				continue
			}
			if r := a.of(o.ID, p); (r.Kind == reasonWrite || r.Kind == reasonStored) && r.blamed(o.ID) != "" && uses(r.RPC, p) {
				return a.index(r.Step)
			}
		}
	}
	return -1
}

func (a attribution) changedWriteBefore(step string) int {
	at := a.index(step)
	if at < 0 || a.changed == nil {
		return -1
	}
	reach := refReach(a.rec)
	for i := 0; i < at; i++ {
		w := a.rec.Steps[i]
		if !isWrite(w) || !a.bad[w.ID] || a.flipped(w) != "" || !related(reach, at, i, w.ID) || !a.reaches(i, at) {
			continue
		}
		for _, p := range a.changed(w.ID) {
			if a.reordered == nil || !a.reordered(w.ID, p) {
				return i
			}
		}
	}
	return -1
}

func (a attribution) reaches(i, at int) bool {
	w, st := a.rec.Steps[i], a.rec.Steps[at]
	c := a.e.contractOf(st.Call)
	if c == nil {
		return true
	}
	if m, err := a.e.cat.Lookup(w.Call); err == nil && slices.Contains(c.Needs, m.FullName) {
		return true
	}
	wb, req := decoded(w.Response), decoded(st.Request)
	sent := map[string]bool{}
	eachLeaf(req, "", func(p string, v any) {
		sent[compactValue(v)] = sent[compactValue(v)] || p != "" && !diff.IDNamedPath(leafOf(p))
	})
	for _, p := range a.changed(w.ID) {
		v, ok := chain.Get(wb, p)
		if eff := c.Effects[leafOf(p)]; eff != nil && eff.Is != contract.EffectNone || ok && sent[compactValue(v)] {
			return true
		}
	}
	return false
}

func (a attribution) sameRecordWriteBefore(step string) int {
	at := a.index(step)
	if at < 0 || a.changed == nil {
		return -1
	}
	mine := stepRefs(a.rec, at)
	fields := leafFields(a.changed(step))
	call := a.rec.Steps[at].Call
	for i := at - 1; i >= 0; i-- {
		w := a.rec.Steps[i]
		if w == nil || !isWrite(w) || !a.bad[w.ID] || len(a.changed(w.ID)) == 0 || !verdictMoved(a.changed(w.ID)) && (w.Call != call || !overlaps(fields, leafFields(a.changed(w.ID)))) {
			continue
		}
		for ref := range stepRefs(a.rec, i) {
			if !mine[ref] {
				continue
			}
			return a.behind(i)
		}
	}
	return -1
}

func leafFields(paths []string) map[string]bool {
	out := map[string]bool{}
	for _, p := range paths {
		segs := chain.SplitPath(p)
		for k := len(segs) - 1; k >= 0; k-- {
			if _, err := strconv.Atoi(segs[k]); err != nil {
				out[segs[k]] = true
				break
			}
		}
	}
	return out
}

func verdictMoved(paths []string) bool {
	return slices.ContainsFunc(paths, envelopeOnly)
}

func overlaps(a, b map[string]bool) bool {
	for f := range a {
		if b[f] {
			return true
		}
	}
	return false
}

func related(reach func(int) map[string]bool, at, i int, id string) bool {
	touched := reach(at)
	return touched[id] || overlaps(reach(i), touched)
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

func (a attribution) echoed(wi int, r *runner.StepRecord, path string) (reason, bool) {
	w := a.rec.Steps[wi]
	if a.e == nil || a.e.cat == nil || a.unchanged == nil || path == "" || envelopeOnly(path) {
		return reason{}, false
	}
	rm, err := a.e.cat.Lookup(r.Call)
	if err != nil {
		return reason{}, false
	}
	wm, err := a.e.cat.Lookup(w.Call)
	if err != nil {
		return reason{}, false
	}
	want, ok := carrierOf(rm, path)
	if !ok {
		return reason{}, false
	}
	var rb, wb any
	if json.Unmarshal(r.Response, &rb) != nil || json.Unmarshal(w.Response, &wb) != nil {
		return reason{}, false
	}
	rv, ok := chain.Get(rb, path)
	if !ok {
		return reason{}, false
	}
	before, agreed := "", false
	if a.was != nil {
		if v, ok := a.was(r.ID, path); ok {
			before, agreed = compactValue(v), true
		}
	}
	wp, wv, found := "", "", false
	eachLeaf(wb, "", func(p string, v any) {
		if found {
			return
		}
		c, ok := carrierOf(wm, p)
		if !ok || envelopeOnly(p) || c != want && !(agreed && leafName(c) == leafName(want) && compactValue(v) == before) {
			return
		}
		if sameEntity(rb, path, wb, p) && a.unchanged(w.ID, p) && compactValue(v) != compactValue(rv) {
			wp, wv, found = p, compactValue(v), true
		}
	})
	if !found {
		return reason{}, false
	}
	agree, confirm := false, false
	for _, o := range a.rec.Steps[wi+1:] {
		if isWrite(o) {
			break
		}
		if o == nil || o == r {
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
				agree = agree || methodName(o.Call) != methodName(r.Call)
			}
		})
	}
	if confirm {
		out := a.own(reasonDiffers, r)
		out.Path, out.Other = path, methodName(w.Call)
		return out, true
	}
	out := a.write(wi)
	out.Kind, out.Path, out.Want, out.Got, out.ReadRPC = reasonStored, wp, wv, compactValue(rv), methodName(r.Call)
	if !agree && agreed && before == wv {
		out.Kind, out.Read, out.ReadRPC, out.Profile = reasonUnclear, r.ID, r.Call, profileAs(a.e, r)
		if sentAs(w, wp, wv) {
			out.Other = asSent
		}
	}
	return out, true
}

const asSent = "as sent"

func sentAs(w *runner.StepRecord, path, value string) bool {
	var req any
	found := false
	if json.Unmarshal(w.Request, &req) == nil {
		eachLeaf(req, "", func(p string, v any) {
			found = found || leafOf(p) == leafOf(path) && compactValue(v) == value
		})
	}
	return found
}

func valueText(s string) string {
	return capText(chain.EdgeQuoted(s), 60)
}

func envelopeOnly(path string) bool {
	var segs []string
	for _, seg := range chain.SplitPath(path) {
		if _, err := strconv.Atoi(seg); err != nil {
			segs = append(segs, seg)
		}
	}
	p := strings.Join(segs, ".")
	list, _, _ := strings.Cut(chain.ItemEnvelope(), "[].")
	item, ok := strings.CutPrefix(p, list+".")
	return p == "" || p == "code" || p == "message" || strings.HasPrefix(p, "transport.") || chain.IsEnvelopePath(p) || ok && list != "" && chain.IsEnvelopePath(item)
}

func (a attribution) unstored(w *runner.StepRecord, path string) reason {
	if a.e == nil || a.e.cat == nil || a.unchanged == nil || path == "" || envelopeOnly(path) || a.unchanged(w.ID, path) {
		return reason{}
	}
	wm, err := a.e.cat.Lookup(w.Call)
	if err != nil {
		return reason{}
	}
	want, ok := carrierOf(wm, path)
	var wb any
	if !ok || json.Unmarshal(w.Response, &wb) != nil {
		return reason{}
	}
	wv, ok := chain.Get(wb, path)
	if !ok {
		return reason{}
	}
	for _, o := range a.rec.Steps[a.index(w.ID)+1:] {
		if isWrite(o) {
			return reason{}
		}
		om, err := a.e.cat.Lookup(o.Call)
		var ob any
		if err != nil || json.Unmarshal(o.Response, &ob) != nil {
			continue
		}
		if list, ok := reorderedList(wb, ob, path); ok && sameEntity(wb, list, ob, list) && a.unchanged(o.ID, path) {
			r := a.own(reasonStoredOrder, w)
			r.Path, r.ReadRPC = list, methodName(o.Call)
			return r
		}
		read, found := "", false
		eachLeaf(ob, "", func(p string, v any) {
			if c, ok := carrierOf(om, p); !found && ok && c == want && sameEntity(wb, path, ob, p) && a.unchanged(o.ID, p) && compactValue(v) != compactValue(wv) {
				read, found = compactValue(v), true
			}
		})
		if found {
			r := a.own(reasonStored, w)
			r.Path, r.Want, r.Got, r.ReadRPC = path, compactValue(wv), read, methodName(o.Call)
			return r
		}
	}
	return reason{}
}

func reorderedList(w, r any, path string) (string, bool) {
	segs := chain.SplitPath(path)
	for i, seg := range segs {
		if _, err := strconv.Atoi(seg); err != nil || i == 0 {
			continue
		}
		prefix := strings.Join(segs[:i], ".")
		wl, wok := chain.Get(w, prefix)
		rl, rok := chain.Get(r, prefix)
		wa, _ := wl.([]any)
		ra, _ := rl.([]any)
		if !wok || !rok || len(wa) < 2 || len(wa) != len(ra) {
			return "", false
		}
		ws, rs := make([]string, len(wa)), make([]string, len(ra))
		for j := range wa {
			ws[j], rs[j] = compactValue(wa[j]), compactValue(ra[j])
		}
		if strings.Join(ws, "\x00") == strings.Join(rs, "\x00") {
			return "", false
		}
		sort.Strings(ws)
		sort.Strings(rs)
		return prefix, strings.Join(ws, "\x00") == strings.Join(rs, "\x00")
	}
	return "", false
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
	parent := func(root any, path string) (map[string]any, []any) {
		segs := chain.SplitPath(path)
		if len(segs) < 2 {
			return nil, nil
		}
		v, _ := chain.Get(root, strings.Join(segs[:len(segs)-1], "."))
		m, _ := v.(map[string]any)
		var list []any
		if _, err := strconv.Atoi(segs[len(segs)-2]); err == nil && len(segs) > 2 {
			l, _ := chain.Get(root, strings.Join(segs[:len(segs)-2], "."))
			list, _ = l.([]any)
		}
		return m, list
	}
	idKey := func(k string) bool {
		lower := strings.ToLower(k)
		return lower == "id" || strings.HasPrefix(lower, "id_") || strings.HasSuffix(lower, "_id")
	}
	distinct := func(list []any, k string) bool {
		seen := map[string]bool{}
		for _, it := range list {
			m, _ := it.(map[string]any)
			v := compactValue(m[k])
			if seen[v] {
				return false
			}
			seen[v] = true
		}
		return true
	}
	x, xl := parent(a, pa)
	y, yl := parent(b, pb)
	matched := false
	for k, v := range x {
		w, ok := y[k]
		if !idKey(k) || !ok || xl == nil && yl == nil || !distinct(xl, k) || !distinct(yl, k) {
			continue
		}
		if compactValue(v) != compactValue(w) {
			return false
		}
		matched = true
	}
	if matched {
		return true
	}
	sawID := false
	for k, v := range x {
		if !idKey(k) {
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
