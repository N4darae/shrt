package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

type literalCollision struct {
	step  string
	index int
	why   string
	field string
	value string
	hint  string

	unnamed bool
	others  []string
}

func (l *literalCollision) line() string {
	if l.unnamed {
		also := ""
		if len(l.others) > 0 {
			also = " (or another literal it sends: " + strings.Join(l.others, ", ") + ")"
		}
		return fmt.Sprintf("the chain collides with itself: step %q was refused as a uniqueness conflict (%s) that names no field, "+
			"and every field it builds from a reference is built from ${uuid} or a clock value, unique to its run, so what collides "+
			"is a literal: %s is the literal %s%s, and every run after the first collides with the record the first one created. "+
			"This is a defect in the chain, not a fixture or backend problem, and a fresh -var does not help: build it from a var, "+
			"e.g. %s: %s", l.step, l.why, l.field, l.value, also, l.field, l.hint)
	}
	return fmt.Sprintf("the chain collides with itself: step %q was refused as a uniqueness conflict (%s), and %s is the literal %s, "+
		"so every run after the first collides with the record the first one created. This is a defect in the chain, not a fixture "+
		"or backend problem, and a fresh -var does not help: build it from a var, e.g. %s: %s", l.step, l.why, l.field, l.value, l.field, l.hint)
}

func detectLiteralCollision(e *env, c *chain.Chain, rec *runner.Record) *literalCollision {
	if c == nil || rec == nil || rec.DryRun {
		return nil
	}
	var first *runner.StepRecord
	index := -1
	for i, st := range rec.Steps {
		if st.Status == runner.StatusFailed || st.Status == runner.StatusError {
			first, index = st, i
			break
		}
	}
	if first == nil {
		return nil
	}
	why := stepRefusalText(first)
	if why == "" || !uniquenessConflict.MatchString(why) {
		return nil
	}
	var req any
	if err := json.Unmarshal(first.Request, &req); err != nil {
		return nil
	}
	folded := foldName(why)
	var byValue, byName []string
	varBuilt, onlyGenerated := false, true
	sent := map[string]string{}
	visitLeaves(req, "", func(path string) {
		v, ok := requestTemplate(c, first.ID, path)
		if !ok {
			return
		}
		value := ""
		if got, ok := chain.Get(req, path); ok && got != nil {
			value = fmt.Sprint(got)
		}
		text, isText := v.(string)
		if isText && requestRef.MatchString(text) {
			if !builtOnlyFromGenerators(text) {
				onlyGenerated = false
			}
			if value != "" && strings.Contains(why, value) {
				varBuilt = true
			}
			if name := foldName(leafName(path)); name != "" && strings.Contains(folded, name) {
				varBuilt = true
			}
			return
		}
		if !isText || len(value) < 3 {
			return
		}
		sent[path] = value
		if strings.Contains(why, value) {
			byValue = append(byValue, path)
		} else if name := foldName(leafName(path)); name != "" && strings.Contains(folded, name) {
			byName = append(byName, path)
		}
	})
	if varBuilt {
		return nil
	}
	pick := byValue
	if len(pick) == 0 {
		pick = byName
	}
	unnamed := false
	if len(pick) == 0 && onlyGenerated {
		for path := range sent {
			pick = append(pick, path)
		}
		unnamed = true
	}
	pick = notAcceptedRepeatedly(e, rec, first, pick, sent)
	if len(pick) == 0 {
		return nil
	}
	sort.Strings(pick)
	field := pick[0]
	l := &literalCollision{step: first.ID, index: index, why: why, field: field, value: sent[field],
		hint: leafName(field) + "-${vars." + suggestedVar(c) + "}"}
	if unnamed {
		for _, path := range pick[1:] {
			l.others = append(l.others, path+"="+sent[path])
		}
	}
	l.unnamed = unnamed
	return l
}

func builtOnlyFromGenerators(text string) bool {
	for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
		if k := chain.ParseRef(m[1]).Kind; k != chain.RefUUID && k != chain.RefClock {
			return false
		}
	}
	return true
}

func leafName(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

func suggestedVar(c *chain.Chain) string {
	names := []string{}
	for n := range isolationVars(c) {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if n == "tag" {
			return n
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return "tag"
}

func notAcceptedRepeatedly(e *env, rec *runner.Record, first *runner.StepRecord, paths []string, sent map[string]string) []string {
	if e == nil || len(paths) == 0 {
		return paths
	}
	accepted := map[string]int{}
	ids, _ := e.store.ListRuns(rec.Chain)
	for _, id := range ids {
		if id == rec.RunID {
			continue
		}
		prev, err := e.store.LoadRun(rec.Chain, id)
		if err != nil || prev.DryRun {
			continue
		}
		st, ok := prev.Step(first.ID)
		if !ok || st.Call != first.Call || !createdStep(st) {
			continue
		}
		var req any
		if json.Unmarshal(st.Request, &req) != nil {
			continue
		}
		for _, path := range paths {
			if got, ok := chain.Get(req, path); ok && got != nil && fmt.Sprint(got) == sent[path] {
				accepted[path]++
			}
		}
	}
	out := []string{}
	for _, path := range paths {
		if accepted[path] < 2 {
			out = append(out, path)
		}
	}
	return out
}
