package contract

import (
	"fmt"
	"regexp"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var keyConflict = regexp.MustCompile(`(?i)idempoten|key ?reuse|key ?conflict|key ?mismatch`)

func (p *Plan) probeIdempotency(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || chain.IsReadOnlyCall(st.Call) {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		var keyField *catalog.Field
		for _, f := range catalog.DescribeMessage(m.Input()).Fields {
			if isIdempotencyField(f) {
				keyField = f
			}
		}
		if keyField == nil {
			continue
		}
		key, ok := namecase.LookupKey(st.Body, keyField.Name)
		if !ok {
			continue
		}
		carrier, idField, numbers := "", "", []string{}
		for _, fd := range catalog.DescribeMessage(m.Output()).Fields {
			if fd.Kind != "message" || fd.Repeated || fd.MapKey != "" || fd.Name == chain.EnvelopeField() || IsVerdictFieldName(fd.Name) {
				continue
			}
			for _, sf := range fd.Fields {
				if idField == "" && IsEntityIDField(sf.Name) && sf.Kind == "string" {
					carrier, idField = fd.Name, sf.Name
				}
			}
			if carrier == fd.Name {
				for _, sf := range fd.Fields {
					if chain.IsNumericKind(sf.Kind) && !sf.Repeated {
						numbers = append(numbers, sf.Name)
					}
				}
			}
		}
		if carrier == "" {
			continue
		}
		p.addIdempotencyProbes(lib, st, m, key, carrier, idField, numbers)
	}
}

func (p *Plan) addIdempotencyProbes(lib *Library, st *chain.Step, m *catalog.Method, key, carrier, idField string, numbers []string) {
	sent := "${steps." + st.ID + ".request." + key + "}"
	first := "${" + st.ID + "." + carrier + "." + idField + "}"
	idPath := carrier + "." + idField

	replay := copyStep(st, p.freeStepID(st.ID+"_replay"))
	replay.Export = nil
	replay.Body[key] = sent
	renameStepRefs(replay, st.ID, replay.ID)
	replay.Body[key] = sent
	replay.Description = fmt.Sprintf("the same request with the same %s returns the %s %s created, not a second one.", key, carrier, st.ID)
	replay.Expect = append(SuccessExpectation(m), chain.Expectation{Path: idPath, Equals: first})

	other := copyStep(replay, p.freeStepID(st.ID+"_replay_other_body"))
	bumped := bumpNumbers(other.Body, catalog.DescribeMessage(m.Input()).Fields)
	other.Description = fmt.Sprintf("the same %s with another body (%s changed) still returns the first %s, unchanged.", key, stepList(bumped), carrier)
	other.Expect = append(SuccessExpectation(m), chain.Expectation{Path: idPath, Equals: first})
	for _, n := range numbers {
		other.Expect = append(other.Expect, chain.Expectation{Path: carrier + "." + n, Equals: "${" + st.ID + "." + carrier + "." + n + "}"})
	}
	conflict := ""
	for _, f := range lib.AllFailures(st.Call) {
		if keyConflict.MatchString(f.Reason) || (keyConflict.MatchString(f.When) && !isUnauthenticated(f)) {
			other.Expect = refusalFor(m, f)
			other.Description = fmt.Sprintf("the same %s with another body (%s changed) is refused with %s.", key, stepList(bumped), f.Label())
			conflict = f.Label()
			break
		}
	}
	added := []*chain.Step{replay}
	if len(bumped) > 0 {
		added = append(added, other)
	}

	c, _ := lib.Get(st.Call)
	required := false
	if c != nil {
		for _, r := range c.Required {
			if namecase.Fold(r) == namecase.Fold(key) {
				required = true
			}
		}
	}
	pair := ""
	if !required {
		a := copyStep(st, p.freeStepID(st.ID+"_no_key"))
		a.Export = nil
		p.freshen(lib, a)
		a.Body[key] = ""
		renameStepRefs(a, st.ID, a.ID)
		a.Description = fmt.Sprintf("no %s: a new %s.", key, carrier)
		a.Expect = append(SuccessExpectation(m), chain.Expectation{Path: idPath, NotEqual: first})
		b := copyStep(a, p.freeStepID(st.ID+"_no_key_2"))
		p.freshen(lib, b)
		b.Body[key] = ""
		renameStepRefs(b, a.ID, b.ID)
		b.Description = fmt.Sprintf("the same request again with no %s is another new %s, not a replay of %s.", key, carrier, a.ID)
		b.Expect = append(SuccessExpectation(m), chain.Expectation{Path: idPath, NotEqual: "${" + a.ID + "." + idPath + "}"})
		added = append(added, a, b)
		pair = fmt.Sprintf(", and %s, %s send none and must create two", a.ID, b.ID)
	}
	p.Chain.Steps = append(p.Chain.Steps, added...)
	what := "must return the first " + carrier
	if conflict != "" {
		what = "is refused with " + conflict + " when the body differs"
	}
	p.note("step %s: %s is an idempotency key; %s replays it with the same body and must return %s's %s, %s replays it with "+
		"another body and %s%s", st.ID, key, replay.ID, st.ID, idField, other.ID, what, pair)
}

func bumpNumbers(body map[string]any, fields []*catalog.Field) []string {
	changed := []string{}
	var walk func(m map[string]any, fs []*catalog.Field, path string)
	walk = func(m map[string]any, fs []*catalog.Field, path string) {
		for _, f := range fs {
			key, ok := namecase.LookupKey(m, f.Name)
			if !ok || f.MapKey != "" || idLike(f.Name) {
				continue
			}
			at := join(path, f.Name)
			switch v := m[key].(type) {
			case []any:
				for i, item := range v {
					if im, ok := item.(map[string]any); ok {
						walk(im, f.Fields, fmt.Sprintf("%s.%d", at, i))
					}
				}
			case map[string]any:
				walk(v, f.Fields, at)
			default:
				if n, ok := numericValue(v); ok && n != 0 && chain.IsNumericKind(f.Kind) {
					m[key] = fmt.Sprint(n + 1)
					changed = append(changed, at)
				}
			}
		}
	}
	walk(body, fields, "")
	return changed
}

func stepList(names []string) string {
	if len(names) == 0 {
		return "nothing"
	}
	out := names[0]
	for _, n := range names[1:] {
		out += ", " + n
	}
	return out
}
