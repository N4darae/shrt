package main

import (
	"encoding/json"
	"fmt"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/runner"
)

func (a attribution) bears(i int, path string) bool {
	w := a.rec.Steps[i]
	if a.bad[w.ID] {
		return true
	}
	if refusalOf(w) != "" {
		return !a.ref
	}
	if a.e == nil || a.e.cat == nil || path == "" {
		return true
	}
	leaf := leafOf(path)
	effects := a.e.effectsOf(w.Call)
	if eff := effects[leaf]; eff != nil && eff.Is != contract.EffectNone {
		return true
	}
	m, err := a.e.cat.Lookup(w.Call)
	return err != nil || effects == nil || declares(m.Output(), leaf, 0)
}

func (a attribution) bearing(at int, path string) int {
	pos := map[string]int{}
	for i, st := range a.rec.Steps {
		if st == nil {
			continue
		}
		if _, seen := pos[st.ID]; !seen {
			pos[st.ID] = i
		}
	}
	for _, i := range entityWrites(a.rec, at, path, a.bad, pos) {
		if a.bears(i, path) {
			return i
		}
	}
	return -1
}

func (a attribution) principal(st *runner.StepRecord, path string) string {
	if a.was == nil || a.unchanged == nil || path == "" || envelopeOnly(path) {
		return ""
	}
	old, ok := a.was(st.ID, path)
	var rb any
	if !ok || json.Unmarshal(st.Response, &rb) != nil {
		return ""
	}
	if _, ok := chain.Get(rb, path); !ok {
		return ""
	}
	at := a.index(st.ID)
	for j, o := range a.rec.Steps {
		var ob any
		if o == nil || o == st || isWrite(o) || o.Call != st.Call || profileOf(o) == profileOf(st) || refusalOf(o) != "" || json.Unmarshal(o.Response, &ob) != nil {
			continue
		}
		v, ok := chain.Get(ob, path)
		if !ok || compactValue(v) != compactValue(old) || !a.unchanged(o.ID, path) || !sameEntity(rb, path, ob, path) || a.changedBetween(min(at, j), max(at, j)) {
			continue
		}
		return fmt.Sprintf("%s answers %s differently as %s than as %s", methodName(st.Call), gateIndex.ReplaceAllString(path, "[]$1"), profileOf(st), profileOf(o))
	}
	return ""
}

func (a attribution) changedBetween(from, to int) bool {
	for _, w := range a.rec.Steps[from+1 : to] {
		if isWrite(w) && a.bad[w.ID] {
			return true
		}
	}
	return false
}

func (a attribution) listedFrom(step, path string) int {
	at := a.index(step)
	var body any
	if at < 0 || json.Unmarshal(a.rec.Steps[at].Response, &body) != nil {
		return -1
	}
	items, _ := chain.Get(body, path)
	list, _ := items.([]any)
	for _, item := range list {
		for _, id := range idsOf(item) {
			if src := producer(a.rec, at, id); src != "" && a.bad[src] {
				if i := a.index(src); i >= 0 && isWrite(a.rec.Steps[i]) {
					return i
				}
			}
		}
	}
	return -1
}
