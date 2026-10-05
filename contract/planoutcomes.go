package contract

import (
	"slices"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var increaseWord = lazyRegexp(`(?i)\b(increase[sd]?|add[sd]?|raise[sd]?|top[s]? up)\b`)

func (p *Plan) assertOutcomes(lib *Library) {
	ids := []string{}
	for st, m := range p.called(notVerdictOnly) {
		c, ok := lib.Get(m.FullName)
		if !ok || len(DeclaredFacts(c)) == 0 {
			continue
		}
		if extra := p.outcomeExpectations(st, m, c); len(extra) > 0 {
			st.Expect = append(st.Expect, extra...)
			ids = append(ids, st.ID)
		}
	}
	if len(ids) > 0 {
		claim := ", so a freshly planned chain passes 'chain lint -strict'"
		if p.verdictOnlyLeft(lib) {
			claim = ""
		}
		p.note("%s %s %s what %s contract declares beyond the verdict%s: "+
			"a reference the request sent read back, the state the contract names, the id a create returns, a stock level "+
			"at least the quantity added, each item of a batch; that is the plan's floor, so add the values your test data "+
			"should produce", pluralVerb(len(ids), "step", "steps"), strings.Join(clipList(ids, 6), ", "),
			pluralVerb(len(ids), "asserts", "assert"), pluralVerb(len(ids), "its", "their"), claim)
	}
}

func (p *Plan) verdictOnlyLeft(lib *Library) bool {
	for _, m := range p.called(notVerdictOnly) {
		if c, ok := lib.Get(m.FullName); ok && len(DeclaredFacts(c)) > 0 {
			return true
		}
	}
	return false
}

func notVerdictOnly(st *chain.Step) bool {
	return st.AllowFail || isRefusalStep(st) || !AssertsOnlyVerdict(st)
}

func (p *Plan) outcomeExpectations(st *chain.Step, m *catalog.Method, c *RPCContract) []chain.Expectation {
	out := []chain.Expectation{}
	read := chain.IsReadOnlyCall(m.FullName)
	inputs := catalog.DescribeMessage(m.Input()).Fields
	carriers := []*catalog.Field{}
	scalars := []*catalog.Field{}
	for _, fd := range catalog.DescribeMessage(m.Output()).Fields {
		switch {
		case fd.Repeated || fd.MapKey != "" || fd.Name == chain.EnvelopeField() || IsVerdictFieldName(fd.Name):
		case fd.Kind == "message":
			carriers = append(carriers, fd)
		default:
			scalars = append(scalars, fd)
		}
	}
	if len(carriers) == 1 {
		car := carriers[0]
		for _, f := range inputs {
			if f.Repeated || f.MapKey != "" || f.Kind == "message" {
				continue
			}
			key, ok := namecase.LookupKey(st.Body, f.Name)
			if !ok {
				continue
			}
			if text, ok := p.stepRef(st.Body[key]); ok && sameScalar(car.Fields, f) {
				out = append(out, chain.Expectation{Path: car.Name + "." + f.Name, Equals: text})
			}
		}
		if !read {
			out = append(out, stateExpectations(car, c)...)
		}
		if len(out) == 0 && !read {
			if i := slices.IndexFunc(car.Fields, func(sub *catalog.Field) bool {
				return IsEntityIDField(sub.Name) && sub.Kind == "string" && !sub.Repeated
			}); i >= 0 {
				out = append(out, chain.Expectation{Path: car.Name + "." + car.Fields[i].Name, NotEmpty: true})
			}
		}
		return out
	}
	if read || len(carriers) > 0 {
		return out
	}
	if list := p.batchOutcomes(st, m, c); len(list) > 0 {
		return list
	}
	if !increaseWord().MatchString(c.Summary) && !c.Effects.increases() {
		return out
	}
	for _, f := range inputs {
		if f.Repeated || !chain.IsNumericKind(f.Kind) || !isQuantityName(f.Name) {
			continue
		}
		key, ok := literalKey(st.Body, f.Name)
		if !ok {
			continue
		}
		for _, o := range scalars {
			if chain.IsNumericKind(o.Kind) && strings.HasPrefix(o.Name, f.Name+"_") && slices.Contains(DeclaredFacts(c), o.Name) {
				out = append(out, chain.Expectation{Path: o.Name, Gte: "${steps." + st.ID + ".request." + key + "}"})
			}
		}
	}
	return out
}

func stateExpectations(car *catalog.Field, c *RPCContract) []chain.Expectation {
	out := []chain.Expectation{}
	for _, sub := range car.Fields {
		if len(sub.EnumValues) < 2 || sub.Repeated {
			continue
		}
		if v := stateIn([]string{c.Exports[car.Name], c.Summary}, sub.EnumValues[1:], enumShort(sub.EnumValues)); v != "" {
			out = append(out, chain.Expectation{Path: car.Name + "." + sub.Name, Equals: v})
		}
	}
	return out
}

func (p *Plan) assertStates(lib *Library) {
	for st, m := range p.called(func(st *chain.Step) bool {
		return st.AllowFail || isRefusalStep(st) || effectOutcome(st) != outcomeSuccess || chain.IsReadOnlyCall(st.Call) || p.streams(st)
	}) {
		c, ok := lib.Get(m.FullName)
		car := singleCarrier(m)
		if !ok || car == nil || len(DeclaredFacts(c)) == 0 {
			continue
		}
		for _, e := range stateExpectations(car, c) {
			if !hasExpectOn(st, e.Path) {
				st.Expect = append(st.Expect, e)
			}
		}
	}
}

func (p *Plan) stepRef(v any) (string, bool) {
	text, _ := v.(string)
	src, isRef := refSource(text)
	return text, isRef && src != "vars" && src != "env" && p.stepByID(src) != nil
}

func sameScalar(fields []*catalog.Field, f *catalog.Field) bool {
	sub := fieldByName(fields, f.Name)
	return sub != nil && sub.Kind == f.Kind && !sub.Repeated
}

func (p *Plan) assertStreamEcho() {
	for st, m := range p.called(func(st *chain.Step) bool { return !p.streams(st) || st.AllowFail || isRefusalStep(st) }) {
		car := singleCarrier(m)
		if car == nil {
			continue
		}
		for _, f := range catalog.DescribeMessage(m.Input()).Fields {
			key, ok := namecase.LookupKey(st.Body, f.Name)
			if !ok || f.Repeated || !IsEntityIDField(f.Name) {
				continue
			}
			path := catalog.StreamMessages + ".0." + car.Name + "." + f.Name
			if text, ok := p.stepRef(st.Body[key]); ok && sameScalar(car.Fields, f) && !hasExpectOn(st, path) {
				st.Expect = append(st.Expect, chain.Expectation{Path: path, Equals: text})
			}
		}
	}
}

func (p *Plan) assertTimestamps(lib *Library) {
	for st, m := range p.called(func(st *chain.Step) bool { return isRefusalStep(st) || st.AllowFail }) {
		c, ok := lib.Get(m.FullName)
		if !ok {
			c = &RPCContract{}
		}
		notes := len(p.Notes)
		extra := p.timestampExpectations(st, m, c, lib.DescriptionOf(lib.Domain(m.FullName)))
		p.Notes = p.Notes[:notes]
		for _, e := range extra {
			if !hasExpectOn(st, e.Path) && !slices.ContainsFunc(st.Expect, func(x chain.Expectation) bool {
				return x.Exists != nil && !*x.Exists && (x.Path == e.Path || strings.HasPrefix(e.Path, x.Path+"."))
			}) {
				st.Expect = append(st.Expect, e)
			}
		}
	}
}

func (p *Plan) batchOutcomes(st *chain.Step, m *catalog.Method, c *RPCContract) []chain.Expectation {
	notList := func(fd *catalog.Field) bool { return !fd.Repeated || fd.Kind != "message" || fd.MapKey != "" }
	lists := slices.DeleteFunc(catalog.DescribeMessage(m.Output()).Fields, notList)
	lines := slices.DeleteFunc(catalog.DescribeMessage(m.Input()).Fields, notList)
	if len(lists) != 1 || len(lines) != 1 {
		return nil
	}
	results := lists[0]
	key, ok := namecase.LookupKey(st.Body, lines[0].Name)
	if !ok {
		return nil
	}
	items, _ := st.Body[key].([]any)
	if len(items) == 0 {
		return nil
	}
	verdict := ""
	if listPath, field, ok := strings.Cut(chain.ItemEnvelope(), "[]."); ok && listPath == results.Name {
		verdict = field
	}
	increase := increaseWord().MatchString(c.Summary) || c.Effects.increases()
	out := []chain.Expectation{}
	for i, raw := range items {
		item, _ := raw.(map[string]any)
		prefix := results.Name + "." + strconv.Itoa(i) + "."
		if verdict != "" {
			out = append(out, chain.Expectation{Path: prefix + verdict, Equals: chain.EnvelopeOK()})
		}
		for _, f := range lines[0].Fields {
			k, ok := namecase.LookupKey(item, f.Name)
			if !ok || f.Repeated || f.Kind == "message" {
				continue
			}
			if text, ok := p.stepRef(item[k]); ok {
				if sameScalar(results.Fields, f) {
					out = append(out, chain.Expectation{Path: prefix + f.Name, Equals: text})
				}
				continue
			}
			if _, literal := numericValue(item[k]); !increase || !literal || !isQuantityName(f.Name) {
				continue
			}
			for _, o := range results.Fields {
				if chain.IsNumericKind(o.Kind) && !o.Repeated && strings.HasPrefix(o.Name, f.Name+"_") {
					out = append(out, chain.Expectation{Path: prefix + o.Name,
						Gte: "${steps." + st.ID + ".request." + key + "." + strconv.Itoa(i) + "." + k + "}"})
				}
			}
		}
	}
	out = append(out, chain.Expectation{Path: results.Name + "." + strconv.Itoa(len(items)), Exists: boolPtr(false)})
	return out
}
