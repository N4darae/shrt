package main

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/runner"
)

var qualifiedName = regexp.MustCompile(`\b(?:[A-Za-z_]\w*\.){2,}([A-Za-z_]\w*\.[A-Za-z_]\w*)\b`)

func transportCause(st *runner.StepRecord) string {
	if st == nil || st.Transport == nil {
		return ""
	}
	msg := strings.Join(strings.Fields(st.Transport.Message), " ")
	if i := strings.LastIndex(msg, ": "); i >= 0 && strings.Count(msg[i:], " ") > 2 {
		msg = msg[i+2:]
	}
	return capText(qualifiedName.ReplaceAllString(msg, "$1"), 80)
}

func (it gateItem) effect() string {
	if it.Times == "" {
		return it.Effect
	}
	return it.Effect + ": " + it.Times
}

func (gr *gateGroup) effectNote() string {
	it := gr.example
	if it.Effect == "" {
		return ""
	}
	note, steps, measured := leafOf(it.Path)+" "+it.effect(), map[string]bool{}, 0
	for _, r := range gr.refs {
		key := r.chain + " " + r.it.Step
		if r.it.Reason.Kind == reasonKnockOn || leafOf(r.it.Path) != leafOf(it.Path) || steps[key] {
			continue
		}
		if it.Times == "" || r.it.Effect != "" && r.it.Times != it.Times {
			return note
		}
		if steps[key] = true; r.it.Effect != "" {
			measured++
		}
	}
	switch {
	case measured == len(steps):
		return note + " on every failing step"
	case measured > 1:
		return note + fmt.Sprintf(" on %d of %d failing steps", measured, len(steps))
	}
	return note
}

func effectOf(a attribution, spot []*runner.StepRecord, it gateItem) (string, string) {
	rec, at, r := a.rec, a.index(it.Step), it.Reason
	if at < 0 || isWrite(rec.Steps[at]) || !r.blames() || r.Kind == reasonKnockOn {
		return "", ""
	}
	suspect, first := map[int]bool{}, at
	for _, s := range append([]reason{r}, r.Or...) {
		if i := a.index(s.Step); i >= 0 && i < at && isWrite(rec.Steps[i]) {
			suspect[i], first = true, min(first, i)
		}
	}
	leaf := leafOf(it.Path)
	if eff := a.e.effectsOf(rec.Steps[first].Call)[leaf]; len(suspect) != 1 || eff == nil || cmp.Or(eff.Increase, eff.Decrease) == "" {
		return "", ""
	}
	now, ids, ok := valueAt(rec.Steps[at], it.Path)
	if !ok {
		return "", ""
	}
	anchor, was := -1, 0.0
	for i := first - 1; i >= 0 && anchor < 0; i-- {
		v, found, clear := entityValue(rec.Steps[i], leaf, ids)
		if !clear {
			return "", ""
		}
		if found {
			anchor, was = i, v
		}
	}
	if anchor < 0 {
		return "", ""
	}
	approved, writes := &runner.Record{Steps: spot}, a.entityWrites(at, it.Path, nil, anchor-1)
	if !slices.Contains(writes, anchor) && isWrite(rec.Steps[anchor]) {
		return "", ""
	}
	for _, j := range writes {
		w := rec.Steps[j]
		sw, _ := approved.Step(w.ID)
		eff := a.e.effectsOf(w.Call)[leaf]
		if j != anchor && !suspect[j] && (eff == nil || eff.Is != contract.EffectNone) && (refusalOf(w) == "" || sw == nil || refusalOf(sw) == "") {
			return "", ""
		}
	}
	sAt, _ := approved.Step(it.Step)
	sAnchor, _ := approved.Step(rec.Steps[anchor].ID)
	sNow, sIDs, ok := valueAt(sAt, it.Path)
	sWas, found, _ := entityValue(sAnchor, leaf, sIDs)
	if !ok || !found {
		return "", ""
	}
	return moved(was, now) + " where the approved run " + moved(sWas, sNow), times(now-was, sNow-sWas)
}

func valueAt(st *runner.StepRecord, path string) (float64, map[string]string, bool) {
	if st == nil {
		return 0, nil, false
	}
	body, segs := decoded(st.Response), chain.SplitPath(path)
	for k, seg := range segs {
		if _, err := strconv.Atoi(seg); err == nil {
			list, _ := chain.Get(body, strings.Join(segs[:k], "."))
			if items, _ := list.([]any); len(items) != 1 {
				return 0, nil, false
			}
		}
	}
	v, _ := chain.Get(body, path)
	n, ok := number(v)
	ids := holderIDs(st, body, path)
	return n, ids, ok && len(ids) > 0
}

func holderIDs(st *runner.StepRecord, body any, path string) map[string]string {
	segs := chain.SplitPath(path)
	if len(segs) > 1 {
		holder, _ := chain.Get(body, strings.Join(segs[:len(segs)-1], "."))
		return idMap(holder)
	}
	if ids := idMap(body); len(ids) > 0 {
		return ids
	}
	return idMap(decoded(st.Request))
}

func sameRecord(a, b map[string]string) bool {
	same := false
	for k, v := range a {
		w, ok := b[k]
		if ok && w != v {
			return false
		}
		same = same || ok
	}
	return same
}

func entityValue(st *runner.StepRecord, leaf string, ids map[string]string) (float64, bool, bool) {
	if st == nil || len(ids) == 0 || st.Status == runner.StatusSkipped || refusalOf(st) != "" {
		return 0, false, true
	}
	body, vals, line := decoded(st.Response), map[string]float64{}, func(p string) int {
		n, _ := strconv.Atoi(strings.Trim(gateIndex.FindString(p), "."))
		return n
	}
	refused, _ := chain.ItemRefusals(body)
	eachLeaf(body, "", func(p string, v any) {
		n, ok := number(v)
		if ok && leafOf(p) == leaf && !slices.ContainsFunc(refused, func(r chain.ItemRefusal) bool { return strings.HasPrefix(p, r.Line+".") }) &&
			sameRecord(ids, holderIDs(st, body, p)) {
			vals[p] = n
		}
	})
	paths := slices.SortedFunc(maps.Keys(vals), func(x, y string) int { return cmp.Compare(line(x), line(y)) })
	if len(paths) == 0 {
		return 0, false, true
	}
	last := paths[len(paths)-1]
	if slices.ContainsFunc(paths, func(p string) bool {
		return vals[p] != vals[last] && gateIndex.ReplaceAllString(p, "[]$1") != gateIndex.ReplaceAllString(last, "[]$1")
	}) {
		return 0, false, false
	}
	return vals[last], true, true
}

func moved(from, to float64) string {
	if from == to {
		return "stayed at " + compactValue(from)
	}
	return fmt.Sprintf("%s from %s to %s", delta(from, to), compactValue(from), compactValue(to))
}

func delta(from, to float64) string {
	if from == to {
		return "stayed"
	}
	return map[bool]string{true: "fell", false: "rose"}[to < from] + " " + compactValue(math.Abs(to-from))
}

func times(d, approved float64) string {
	if d == 0 || approved == 0 || d == approved || d > 0 != (approved > 0) {
		return ""
	}
	for q := 1.0; q <= 4; q++ {
		if p := d / approved * q; math.Abs(p-math.Round(p)) < 1e-9 {
			return strings.TrimSuffix(compactValue(math.Round(p))+"/"+compactValue(q), "/1") + "x"
		}
	}
	return ""
}
