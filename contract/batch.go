package contract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var perItemFailure = regexp.MustCompile(`(?i)\bon that (?:line|item|entry)\b|\b(?:line|item|entry) only\b|\bper[- ](?:line|item|entry)\b|\bindependently\b`)

func (p *Plan) probeBatch(lib *Library, isTarget func(*chain.Step) bool) {
	listPath, verdict, ok := strings.Cut(chain.ItemEnvelope(), "[].")
	if !ok || listPath == "" || verdict == "" {
		return
	}
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || chain.IsReadOnlyCall(st.Call) {
			continue
		}
		c, ok := lib.Get(st.Call)
		if !ok {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		var results *catalog.Field
		for _, f := range catalog.DescribeMessage(m.Output()).Fields {
			if f.Name == listPath && f.Repeated && f.Kind == "message" {
				results = f
			}
		}
		if results == nil {
			continue
		}
		stated := perItemFailure.MatchString(c.Summary)
		for _, f := range lib.AllFailures(st.Call) {
			stated = stated || perItemFailure.MatchString(f.When)
		}
		if !stated {
			continue
		}
		p.addPartialBatch(lib, st, c, m, results, listPath, verdict)
	}
}

func (p *Plan) addPartialBatch(lib *Library, st *chain.Step, c *RPCContract, m *catalog.Method, results *catalog.Field, listPath, verdict string) {
	for _, rf := range catalog.DescribeMessage(m.Input()).Fields {
		if !rf.Repeated || rf.Kind != "message" || rf.MapKey != "" {
			continue
		}
		key, ok := namecase.LookupKey(st.Body, rf.Name)
		if !ok {
			continue
		}
		items, _ := st.Body[key].([]any)
		if len(items) == 0 {
			continue
		}
		first, ok1 := items[0].(map[string]any)
		last, ok2 := items[len(items)-1].(map[string]any)
		if !ok1 || !ok2 {
			continue
		}
		for _, sub := range rf.Fields {
			subKey, ok := namecase.LookupKey(last, sub.Name)
			if !ok || sub.Repeated || !chain.IsNumericKind(sub.Kind) || idLike(sub.Name) {
				continue
			}
			min, failure, found := statedMinimum(lib, st.Call, c, sub.Name)
			if !found || failure == nil {
				continue
			}
			bad, _ := cloneBody(last).(map[string]any)
			bad[subKey] = strconv.FormatInt(min-1, 10)
			good, _ := cloneBody(last).(map[string]any)
			partial := copyStep(st, p.freeStepID(st.ID+"_partial"))
			partial.Export = nil
			partial.Body[key] = []any{cloneBody(first), bad, good}
			partial.Description = fmt.Sprintf("%s.1 has %s %d and is refused with %s on that line only; the lines around it are applied.",
				rf.Name, sub.Name, min-1, failure.Label())
			okValue := chain.EnvelopeOK()
			partial.Expect = append(SuccessExpectation(m),
				chain.Expectation{Path: listPath + ".0." + verdict, Equals: okValue},
				chain.Expectation{Path: listPath + ".1." + verdict, NotEqual: okValue})
			codes, _ := codeExpectations(results.Fields, listPath+".1.", *failure)
			partial.Expect = append(partial.Expect, codes...)
			partial.Expect = append(partial.Expect,
				chain.Expectation{Path: listPath + ".2." + verdict, Equals: okValue},
				chain.Expectation{Path: listPath + ".3", Exists: boolPtr(false)})
			p.Chain.Steps = append(p.Chain.Steps, partial)
			reads := p.reportedMatchesStored(lib, partial, key, results, listPath)
			p.Chain.Steps = append(p.Chain.Steps, reads...)
			p.note("step %s: %s sends three %s with the middle one refused (%s %d, %s declared per line); it asserts each line's own "+
				"verdict, and %s assert that what each applied line reports is what is stored, so a batch that stops at the refused line, "+
				"applies it, or reports stale values for later lines fails", st.ID, partial.ID, rf.Name, sub.Name, min-1, failure.Label(), stepIDList(reads))
			return
		}
	}
	p.note("step %s: its contract declares failures reported per item, but no numeric field of a repeated request item has a "+
		"minimum stated in a failure's when:, so no batch with a refused middle item was planned: write one by hand", st.ID)
}

func (p *Plan) reportedMatchesStored(lib *Library, partial *chain.Step, key string, results *catalog.Field, listPath string) []*chain.Step {
	items, _ := partial.Body[key].([]any)
	lastIndex := map[string]int{}
	entity := map[string]entityRead{}
	order := []string{}
	for i, raw := range items {
		if i == 1 {
			continue
		}
		for _, ref := range allStepRefs(raw) {
			prod := p.stepByID(ref[0])
			if prod == nil || chain.IsReadOnlyCall(prod.Call) {
				continue
			}
			if _, seen := entity[ref[0]]; !seen {
				e, ok := p.readerFor(lib, prod, ref[1])
				if !ok {
					continue
				}
				entity[ref[0]] = e
				order = append(order, ref[0])
			}
			lastIndex[ref[0]] = i
		}
	}
	reported := map[string]bool{}
	for _, f := range results.Fields {
		if chain.IsNumericKind(f.Kind) && !f.Repeated {
			reported[f.Name] = true
		}
	}
	out := []*chain.Step{}
	for _, id := range order {
		e := entity[id]
		base := defaultID(e.reader.Name)
		if pm, err := p.cat.Lookup(e.producer.Call); err == nil {
			if suffix := strings.TrimPrefix(e.producer.ID, defaultID(pm.Name)); isIndexSuffix(suffix) {
				base += suffix
			}
		}
		body := catalog.ScaffoldWith(e.reader.Input(), catalog.ScaffoldOptions{})
		setBodyPath(body, e.field, "${"+e.producer.ID+"."+e.idPath+"}")
		read := &chain.Step{
			ID:     p.freeStepID(base + "_after_" + partial.ID),
			Call:   e.reader.FullName,
			Auth:   e.contract.Auth,
			Body:   body,
			Expect: SuccessExpectation(e.reader),
		}
		names := []string{}
		for _, name := range e.scalars {
			if reported[name] {
				path := fmt.Sprintf("%s.%d.%s", listPath, lastIndex[id], name)
				read.Expect = append(read.Expect, chain.Expectation{Path: e.carrier + "." + name, Equals: "${" + partial.ID + "." + path + "}"})
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			continue
		}
		read.Description = fmt.Sprintf("the stored %s after %s is what its line %d reported.", strings.Join(names, ", "), partial.ID, lastIndex[id])
		out = append(out, read)
	}
	return out
}
