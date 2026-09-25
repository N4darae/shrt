package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func stepRefs(st *runner.StepRecord) map[string]bool {
	out := map[string]bool{}
	for _, ref := range st.BodyRefs {
		for _, m := range gateRef.FindAllStringSubmatch(ref, -1) {
			switch m[1] {
			case "vars", "env", "exports":
			default:
				out[m[1]] = true
			}
		}
	}
	return out
}

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
	closure := map[int]map[string]bool{}
	var reach func(i int) map[string]bool
	reach = func(i int) map[string]bool {
		if c, ok := closure[i]; ok {
			return c
		}
		c := map[string]bool{}
		closure[i] = c
		for ref := range stepRefs(rec.Steps[i]) {
			c[ref] = true
			if j, ok := pos[ref]; ok && j < i {
				for r := range reach(j) {
					c[r] = true
				}
			}
		}
		return c
	}
	entities := stepRefs(rec.Steps[at])
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

func requestLine(rec *runner.Record, step string, bad map[string]bool) string {
	st, ok := rec.Step(step)
	if !ok || st == nil {
		return ""
	}
	lead := step
	if i, _ := suspectWrite(rec, step, bad); i >= 0 {
		st = rec.Steps[i]
		lead = fmt.Sprintf("suspect write %s (%s)", st.ID, shortRPC(st.Call))
	}
	if st.AuthProfile != "" && st.AuthProfile != "default" {
		lead += " as " + st.AuthProfile
	}
	var b bytes.Buffer
	if len(st.Request) == 0 || json.Compact(&b, st.Request) != nil {
		return ""
	}
	return lead + " sent " + capText(b.String(), 300)
}
