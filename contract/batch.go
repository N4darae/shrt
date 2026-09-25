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

type batchLine struct {
	item    any
	refused *Failure
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
			bad := func(item map[string]any) map[string]any {
				b, _ := cloneBody(item).(map[string]any)
				b[subKey] = strconv.FormatInt(min-1, 10)
				return b
			}
			said := []string{}
			partial, reads := p.batchProbe(lib, st, m, key, results, listPath, verdict, "partial",
				fmt.Sprintf("%s.1 has %s %d and is refused with %s on that line only; the lines around it are applied.", rf.Name, sub.Name, min-1, failure.Label()),
				[]batchLine{{item: cloneBody(first)}, {item: bad(last), refused: failure}, {item: cloneBody(last)}})
			p.note("step %s: %s sends three %s with the middle one refused (%s %d, %s declared per line); it asserts each line's own "+
				"verdict, and %s assert that what each applied line reports is what is stored, so a batch that stops at the refused line, "+
				"applies it, or reports stale values for later lines fails", st.ID, partial.ID, rf.Name, sub.Name, min-1, failure.Label(), stepIDList(reads))
			if len(items) > 1 {
				firstProbe, _ := p.batchProbe(lib, st, m, key, results, listPath, verdict, "partial_first",
					fmt.Sprintf("%s.0 has %s %d and is refused with %s on that line only; the line after it is applied.", rf.Name, sub.Name, min-1, failure.Label()),
					[]batchLine{{item: bad(first), refused: failure}, {item: cloneBody(last)}})
				lastProbe, _ := p.batchProbe(lib, st, m, key, results, listPath, verdict, "partial_last",
					fmt.Sprintf("the last of %s has %s %d and is refused with %s on that line only; the line before it is applied.", rf.Name, sub.Name, min-1, failure.Label()),
					[]batchLine{{item: cloneBody(first)}, {item: bad(last), refused: failure}})
				said = append(said, firstProbe.ID+" (the refused line first)", lastProbe.ID+" (the refused line last)")
			}
			if probe := p.unknownBatchLine(lib, st, c, m, rf, key, first, last, results, listPath, verdict); probe != "" {
				said = append(said, probe)
			}
			if len(said) > 0 {
				p.note("step %s: %s also refuse one line each; what a refused line alone names is read before and after and must be unchanged, "+
					"and what an applied line names is read after and must be what the line reported, so a batch that checks only the "+
					"middle line, stops at a refused first line, applies an unknown id, or drops the line after a refusal fails", st.ID, strings.Join(said, ", "))
			}
			return
		}
	}
	p.note("step %s: its contract declares failures reported per item, but no numeric field of a repeated request item has a "+
		"minimum stated in a failure's when:, so no batch with a refused middle item was planned: write one by hand", st.ID)
}

func (p *Plan) unknownBatchLine(lib *Library, st *chain.Step, c *RPCContract, m *catalog.Method, rf *catalog.Field, key string, first, last map[string]any,
	results *catalog.Field, listPath, verdict string) string {
	failures := []Failure{}
	for _, f := range lib.AllFailures(st.Call) {
		if f.Unreachable != "" || isUnauthenticated(f) || f.ConnectCode == invalidArgCode || !perItemFailure.MatchString(f.When) {
			continue
		}
		if notFoundReason.MatchString(f.Reason) || notFoundWhen.MatchString(f.When) {
			failures = append(failures, f)
		}
	}
	if len(failures) == 0 {
		return ""
	}
	for _, sub := range rf.Fields {
		name := rf.Name + "." + sub.Name
		fc := c.Fields[name]
		if fc == nil || fc.From == "" || fc.CheckedBy == CheckedByNone {
			continue
		}
		ref, err := ParseRef(fc.From)
		if err != nil {
			continue
		}
		f, found := unknownIDFailure(failures, name, ref, true)
		if !found {
			continue
		}
		subKey, ok := namecase.LookupKey(last, sub.Name)
		text, _ := last[subKey].(string)
		if !ok || !wholeReference(text) {
			continue
		}
		unknown, _ := cloneBody(last).(map[string]any)
		unknown[subKey] = text + unknownIDSuffix
		items := []batchLine{{item: cloneBody(first)}, {item: unknown, refused: &f}}
		probe, _ := p.batchProbe(lib, st, m, key, results, listPath, verdict, "unknown_"+sub.Name+"_line",
			fmt.Sprintf("the last of %s names no existing record (a real id with %q appended) and is refused with %s on that line only; the line before it is applied.",
				rf.Name, unknownIDSuffix, f.Label()), items)
		return fmt.Sprintf("%s (an unknown %s on the last line, %s)", probe.ID, sub.Name, f.Label())
	}
	return ""
}

func (p *Plan) batchProbe(lib *Library, st *chain.Step, m *catalog.Method, key string, results *catalog.Field, listPath, verdict, suffix, desc string,
	lines []batchLine) (*chain.Step, []*chain.Step) {
	probe := copyStep(st, p.freeStepID(st.ID+"_"+suffix))
	probe.Export = nil
	items := make([]any, len(lines))
	okValue := chain.EnvelopeOK()
	probe.Expect = SuccessExpectation(m)
	refused := map[int]bool{}
	for i, l := range lines {
		items[i] = l.item
		at := fmt.Sprintf("%s.%d.", listPath, i)
		if l.refused == nil {
			probe.Expect = append(probe.Expect, chain.Expectation{Path: at + verdict, Equals: okValue})
			continue
		}
		refused[i] = true
		probe.Expect = append(probe.Expect, chain.Expectation{Path: at + verdict, NotEqual: okValue})
		codes, _ := codeExpectations(results.Fields, at, *l.refused)
		probe.Expect = append(probe.Expect, codes...)
	}
	probe.Expect = append(probe.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", listPath, len(lines)), Exists: boolPtr(false)})
	probe.Body[key] = items
	probe.Description = desc
	before, after := p.refusedLineReads(lib, probe, lines)
	p.Chain.Steps = append(p.Chain.Steps, before...)
	p.Chain.Steps = append(p.Chain.Steps, probe)
	reads := p.reportedMatchesStored(lib, probe, key, results, listPath, refused)
	p.Chain.Steps = append(p.Chain.Steps, reads...)
	p.Chain.Steps = append(p.Chain.Steps, after...)
	return probe, append(reads, after...)
}

func (p *Plan) refusedLineReads(lib *Library, probe *chain.Step, lines []batchLine) ([]*chain.Step, []*chain.Step) {
	applied := map[string]bool{}
	for _, l := range lines {
		if l.refused == nil {
			for _, ref := range allStepRefs(l.item) {
				applied[ref[0]] = true
			}
		}
	}
	before, after := []*chain.Step{}, []*chain.Step{}
	seen := map[string]bool{}
	for _, l := range lines {
		if l.refused == nil {
			continue
		}
		for _, ref := range allStepRefs(l.item) {
			if applied[ref[0]] || seen[ref[0]] {
				continue
			}
			seen[ref[0]] = true
			prod := p.stepByID(ref[0])
			if prod == nil || chain.IsReadOnlyCall(prod.Call) {
				continue
			}
			e, ok := p.readerFor(lib, prod, ref[1])
			if !ok {
				continue
			}
			base := defaultID(e.reader.Name)
			if pm, err := p.cat.Lookup(prod.Call); err == nil {
				if suffix := strings.TrimPrefix(prod.ID, defaultID(pm.Name)); isIndexSuffix(suffix) {
					base += suffix
				}
			}
			body := catalog.ScaffoldWith(e.reader.Input(), catalog.ScaffoldOptions{})
			setBodyPath(body, e.field, "${"+prod.ID+"."+e.idPath+"}")
			read := &chain.Step{
				ID:          p.freeStepID(base + "_before_" + probe.ID),
				Description: fmt.Sprintf("the %s as it stands before %s, whose line naming it is refused.", e.carrier, probe.ID),
				Call:        e.reader.FullName,
				Auth:        e.contract.Auth,
				Body:        body,
				Expect:      SuccessExpectation(e.reader),
			}
			check := copyStep(read, p.freeStepID(base+"_after_"+probe.ID))
			check.Description = fmt.Sprintf("the %s after %s is unchanged: its line was refused, so %s read as in %s.", e.carrier, probe.ID, strings.Join(e.scalars, ", "), read.ID)
			for _, name := range e.scalars {
				path := e.carrier + "." + name
				check.Expect = append(check.Expect, chain.Expectation{Path: path, Equals: "${" + read.ID + "." + path + "}"})
			}
			before = append(before, read)
			after = append(after, check)
		}
	}
	return before, after
}

func (p *Plan) reportedMatchesStored(lib *Library, partial *chain.Step, key string, results *catalog.Field, listPath string, refused map[int]bool) []*chain.Step {
	items, _ := partial.Body[key].([]any)
	lastIndex := map[string]int{}
	entity := map[string]entityRead{}
	order := []string{}
	for i, raw := range items {
		if refused[i] {
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
