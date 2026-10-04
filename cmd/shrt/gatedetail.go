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
	note, steps, measured, rest := "; "+leafOf(it.Path)+" "+it.effect(), map[string]bool{}, map[string]bool{}, []gateItem{}
	for _, r := range gr.refs {
		key := r.chain + " " + r.it.Step
		if r.it.Reason.Kind == reasonKnockOn || leafOf(r.it.Path) != leafOf(it.Path) || steps[key] {
			continue
		}
		if it.Times == "" || r.it.Effect != "" && r.it.Times != it.Times {
			return note
		}
		steps[key] = true
		if r.it.Effect != "" {
			measured[key] = true
		} else {
			rest = append(rest, r.it)
		}
	}
	switch {
	case len(measured) == len(steps):
		return note + " on every failing step"
	case len(measured) > 1:
		more := ""
		if len(rest) > 1 {
			more = fmt.Sprintf(" (+%d more)", len(rest)-1)
		}
		return note + fmt.Sprintf(" on %d of %d failing steps; %s %s%s", len(measured), len(steps), rest[0].Step, cmp.Or(rest[0].Unmeasured, "is not measured"), more)
	}
	return note
}

const noEarlier, notMeasured = "has no earlier value of that record to measure from", "is not measured"

func effectOf(a attribution, spot []*runner.StepRecord, it gateItem) (string, string, string) {
	rec, at, r := a.rec, a.index(it.Step), it.Reason
	if at < 0 || isWrite(rec.Steps[at]) || !r.blames() || r.Kind == reasonKnockOn {
		return "", "", ""
	}
	suspect, first := map[int]bool{}, at
	for _, s := range append([]reason{r}, r.Or...) {
		if i := a.index(s.Step); i >= 0 && i < at && isWrite(rec.Steps[i]) {
			suspect[i], first = true, min(first, i)
		}
	}
	if len(suspect) != 1 {
		return "", "", notMeasured
	}
	now, ids, why := valueAt(rec.Steps[at], it.Path)
	if why != "" {
		return "", "", why
	}
	leaf, anchor, was := leafOf(it.Path), -1, 0.0
	for i := first - 1; i >= 0 && anchor < 0; i-- {
		v, found, clear := entityValue(rec.Steps[i], leaf, ids)
		if !clear {
			return "", "", noEarlier
		}
		if found {
			anchor, was = i, v
		}
	}
	if anchor < 0 {
		return "", "", noEarlier
	}
	approved, writes := &runner.Record{Steps: spot}, a.entityWrites(at, it.Path, nil, anchor-1)
	if !slices.Contains(writes, anchor) && isWrite(rec.Steps[anchor]) {
		return "", "", "follows " + rec.Steps[anchor].ID + ", which may move it too"
	}
	for _, j := range writes {
		w := rec.Steps[j]
		sw, _ := approved.Step(w.ID)
		eff := a.e.effectsOf(w.Call)[leaf]
		if j != anchor && !suspect[j] && (eff == nil || eff.Is != contract.EffectNone) && (refusalOf(w) == "" || sw == nil || refusalOf(sw) == "") {
			return "", "", "follows " + w.ID + ", which may move it too"
		}
	}
	sAt, _ := approved.Step(it.Step)
	sAnchor, _ := approved.Step(rec.Steps[anchor].ID)
	sNow, sIDs, why := valueAt(sAt, it.Path)
	sWas, found, _ := entityValue(sAnchor, leaf, sIDs)
	if why != "" || !found {
		return "", "", noEarlier
	}
	return moved(was, now) + " where the approved run " + moved(sWas, sNow), times(now-was, sNow-sWas), ""
}

func valueAt(st *runner.StepRecord, path string) (float64, []string, string) {
	if st == nil {
		return 0, nil, notMeasured
	}
	body, segs := decoded(st.Response), chain.SplitPath(path)
	for k, seg := range segs {
		if _, err := strconv.Atoi(seg); err == nil {
			list, _ := chain.Get(body, strings.Join(segs[:k], "."))
			if items, _ := list.([]any); len(items) != 1 {
				return 0, nil, "reads it in a list of several records"
			}
		}
	}
	v, _ := chain.Get(body, path)
	n, ok := number(v)
	if ids := holderIDs(st, body, path); ok && len(ids) > 0 {
		return n, ids, ""
	}
	return 0, nil, notMeasured
}

func holderIDs(st *runner.StepRecord, body any, path string) []string {
	segs := chain.SplitPath(path)
	if len(segs) > 1 {
		holder, _ := chain.Get(body, strings.Join(segs[:len(segs)-1], "."))
		return idsOf(holder)
	}
	if ids := idsOf(body); len(ids) > 0 {
		return ids
	}
	return idsOf(decoded(st.Request))
}

func entityValue(st *runner.StepRecord, leaf string, ids []string) (float64, bool, bool) {
	if st == nil || st.Status == runner.StatusSkipped || refusalOf(st) != "" {
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
			slices.ContainsFunc(holderIDs(st, body, p), func(id string) bool { return slices.Contains(ids, id) }) {
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
	verb := map[bool]string{true: "fell", false: "rose"}[to < from]
	return fmt.Sprintf("%s %s from %s to %s", verb, compactValue(math.Abs(to-from)), compactValue(from), compactValue(to))
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
