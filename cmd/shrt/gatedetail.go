package main

import (
	"fmt"
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
	note, steps, measured := "; "+leafOf(it.Path)+" "+it.effect(), map[string]bool{}, map[string]bool{}
	for _, r := range gr.refs {
		if r.it.Reason.Kind == reasonKnockOn {
			continue
		}
		if it.Times == "" || r.it.Effect != "" && r.it.Times != it.Times {
			return note
		}
		key := r.chain + " " + r.it.Step
		steps[key] = true
		if r.it.Effect != "" {
			measured[key] = true
		}
	}
	switch {
	case len(measured) == len(steps):
		return note + " on every failing step"
	case len(measured) > 1:
		return note + fmt.Sprintf(" on %d of %d failing steps (no measure for the rest)", len(measured), len(steps))
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
	now, ids, ok := valueAt(rec.Steps[at], it.Path)
	if len(suspect) == 0 || !ok {
		return "", ""
	}
	leaf, anchor, was := leafOf(it.Path), -1, 0.0
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
	approved := &runner.Record{Steps: spot}
	for _, j := range a.entityWrites(at, it.Path, nil, anchor) {
		w := rec.Steps[j]
		sw, _ := approved.Step(w.ID)
		eff := a.e.effectsOf(w.Call)[leaf]
		if !suspect[j] && (eff == nil || eff.Is != contract.EffectNone) && (refusalOf(w) == "" || sw == nil || refusalOf(sw) == "") {
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

func valueAt(st *runner.StepRecord, path string) (float64, []string, bool) {
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
	var vals []float64
	if st != nil && st.Status != runner.StatusSkipped && refusalOf(st) == "" {
		body := decoded(st.Response)
		eachLeaf(body, "", func(p string, v any) {
			n, ok := number(v)
			if ok && leafOf(p) == leaf && slices.ContainsFunc(holderIDs(st, body, p), func(id string) bool { return slices.Contains(ids, id) }) {
				vals = append(vals, n)
			}
		})
	}
	if len(vals) == 0 {
		return 0, false, true
	}
	if slices.ContainsFunc(vals, func(v float64) bool { return v != vals[0] }) {
		return 0, false, false
	}
	return vals[0], true, true
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
