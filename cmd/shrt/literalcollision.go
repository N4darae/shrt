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
}

func (l *literalCollision) line() string {
	return fmt.Sprintf("the chain collides with itself: step %q was refused as a uniqueness conflict (%s), and %s is the literal %s, "+
		"so every run after the first collides with the record the first one created. This is a defect in the chain, not a fixture "+
		"or backend problem, and a fresh -var does not help: build it from a var, e.g. %s: %s", l.step, l.why, l.field, l.value, l.field, l.hint)
}

func detectLiteralCollision(c *chain.Chain, rec *runner.Record) *literalCollision {
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
	varBuilt := false
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
	if len(pick) == 0 {
		return nil
	}
	sort.Strings(pick)
	field := pick[0]
	return &literalCollision{step: first.ID, index: index, why: why, field: field, value: sent[field],
		hint: leafName(field) + "-${vars." + suggestedVar(c) + "}"}
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
