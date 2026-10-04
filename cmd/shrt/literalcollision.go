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
	earlier string
}

func (l *literalCollision) line() string {
	if l.earlier != "" {
		return fmt.Sprintf("the chain collides with itself within one run: step %q was refused as a uniqueness conflict (%s), "+
			"and it sent %s=%s, the value step %q of this same run sent there and the backend accepted, so the second create "+
			"is refused on every run, whatever the vars. This is a defect in the chain, not a fixture or backend problem, and a "+
			"fresh -var does not help: make the two steps send different values, e.g. %s: %s",
			l.step, l.why, l.field, l.value, l.earlier, l.field, l.hint)
	}
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
	if l := collisionWithinRun(e, c, rec, first, index, why, req); l != nil {
		return l
	}
	folded := foldName(why)
	var byValue, byName []string
	varBuilt, onlyGenerated := false, true
	sent := map[string]string{}
	eachLeaf(req, "", func(path string, _ any) {
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
		if !isText || value == "" {
			return
		}
		switch name := foldName(leafName(path)); {
		case quotesValue(why, value):
			sent[path] = value
			byValue = append(byValue, path)
		case name != "" && strings.Contains(folded, name):
			sent[path] = value
			byName = append(byName, path)
		case len(value) >= 3:
			sent[path] = value
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
	names := sortedKeys(isolationVars(c))
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

func quotesValue(why, value string) bool {
	if len(value) >= 3 {
		return strings.Contains(why, value)
	}
	for _, word := range strings.Fields(why) {
		if strings.Trim(word, "\"'`()[]{}<>,;:.!?") == value {
			return true
		}
	}
	return false
}

func notAcceptedRepeatedly(e *env, rec *runner.Record, first *runner.StepRecord, paths []string, sent map[string]string) []string {
	if e == nil || len(paths) == 0 {
		return paths
	}
	ids, _ := e.store.ListRuns(rec.Chain)
	runs := []*runner.Record{}
	for _, id := range ids {
		if id == rec.RunID {
			continue
		}
		prev, err := e.store.LoadRun(rec.Chain, id)
		if err == nil && !prev.DryRun && ranBefore(prev, rec) {
			runs = append(runs, prev)
		}
	}
	sort.SliceStable(runs, func(a, b int) bool { return ranBefore(runs[a], runs[b]) })
	lastAccepted, twice := map[string]bool{}, map[string]bool{}
	for _, prev := range runs {
		st, ok := prev.Step(first.ID)
		if !ok || st.Call != first.Call || st.Status == runner.StatusSkipped || len(st.Response) == 0 && st.HTTPStatus == 0 {
			continue
		}
		var req any
		if json.Unmarshal(st.Request, &req) != nil {
			continue
		}
		created := createdStep(st)
		for _, path := range paths {
			got, ok := chain.Get(req, path)
			if !ok || got == nil || fmt.Sprint(got) != sent[path] {
				continue
			}
			if created && lastAccepted[path] {
				twice[path] = true
			}
			lastAccepted[path] = created
		}
	}
	out := []string{}
	for _, path := range paths {
		if !twice[path] {
			out = append(out, path)
		}
	}
	return out
}

func collisionWithinRun(e *env, c *chain.Chain, rec *runner.Record, first *runner.StepRecord, index int, why string, req any) *literalCollision {
	sent := map[string]string{}
	var quoted, named, all []string
	folded := foldName(why)
	eachLeaf(req, "", func(path string, _ any) {
		got, ok := chain.Get(req, path)
		text, isText := got.(string)
		if !ok || !isText || text == "" {
			return
		}
		switch name := foldName(leafName(path)); {
		case quotesValue(why, text):
			quoted = append(quoted, path)
		case name != "" && strings.Contains(folded, name):
			named = append(named, path)
		case len(text) < 3:
			return
		}
		sent[path] = text
		all = append(all, path)
	})
	pick, every := quoted, false
	if len(pick) == 0 {
		pick = named
	}
	if len(pick) == 0 {
		pick, every = all, true
	}
	sort.Strings(pick)
	for j := 0; j < index && j < len(rec.Steps); j++ {
		st := rec.Steps[j]
		if st == nil || st.Call != first.Call || !createdStep(st) {
			continue
		}
		var before any
		if json.Unmarshal(st.Request, &before) != nil {
			continue
		}
		same := func(path string) bool {
			got, ok := chain.Get(before, path)
			return ok && fmt.Sprint(got) == sent[path]
		}
		if every && !allSame(pick, same) {
			continue
		}
		for _, path := range pick {
			if same(path) && !referencesSentRequest(c, first.ID, path) && !repeatAcceptedBefore(e, rec, first.ID, st.ID, path) {
				hint := leafName(path) + "-${vars." + suggestedVar(c) + "}-2"
				if v, ok := requestTemplate(c, first.ID, path); ok {
					if text, isText := v.(string); isText && text != "" {
						hint = text + "-2"
					}
				}
				return &literalCollision{step: first.ID, index: index, why: why, field: path, value: sent[path], hint: hint, earlier: st.ID}
			}
		}
	}
	return nil
}

func allSame(paths []string, same func(string) bool) bool {
	for _, path := range paths {
		if !same(path) {
			return false
		}
	}
	return len(paths) > 0
}

func referencesSentRequest(c *chain.Chain, step, path string) bool {
	v, ok := requestTemplate(c, step, path)
	text, isText := v.(string)
	if !ok || !isText {
		return false
	}
	for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
		r := chain.ParseRef(m[1])
		if r.Kind == chain.RefStep && (r.Rest == "request" || strings.HasPrefix(r.Rest, "request.")) {
			return true
		}
	}
	return false
}

func repeatAcceptedBefore(e *env, rec *runner.Record, step, earlier, path string) bool {
	if e == nil || e.store == nil {
		return false
	}
	accepted := func(steps []*runner.StepRecord) bool {
		var now, then *runner.StepRecord
		for _, st := range steps {
			switch {
			case st == nil:
			case st.ID == step:
				now = st
			case st.ID == earlier:
				then = st
			}
		}
		if !createdStep(now) || !createdStep(then) || now.Call != then.Call {
			return false
		}
		var a, b any
		if json.Unmarshal(now.Request, &a) != nil || json.Unmarshal(then.Request, &b) != nil {
			return false
		}
		x, okA := chain.Get(a, path)
		y, okB := chain.Get(b, path)
		return okA && okB && x != nil && fmt.Sprint(x) == fmt.Sprint(y)
	}
	if spot, err := e.store.LoadSafeSpot(rec.Chain); err == nil && accepted(spot.Steps) {
		return true
	}
	ids, _ := e.store.ListRuns(rec.Chain)
	for _, id := range ids {
		if id == rec.RunID {
			continue
		}
		prev, err := e.store.LoadRun(rec.Chain, id)
		if err == nil && !prev.DryRun && accepted(prev.Steps) {
			return true
		}
	}
	return false
}
