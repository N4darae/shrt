package contract

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var perItemFailure = lazyRegexp(`(?i)\bon that (?:line|item|entry)\b|\b(?:line|item|entry) (?:only|alone)\b|\bper[- ](?:line|item|entry)\b|\bindependently\b|\bthe others? (?:still )?(?:appl|succeed|go through)`)

func (p *Plan) probeBatch(lib *Library, isTarget func(*chain.Step) bool) {
	listPath, verdict, ok := strings.Cut(chain.ItemEnvelope(), "[].")
	if !ok || listPath == "" || verdict == "" {
		return
	}
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || chain.IsReadOnlyCall(st.Call) {
			continue
		}
		c, m, ok := p.contractOf(lib, st.Call)
		if !ok {
			continue
		}
		results := p.perItemResults(lib, st.Call, c, m)
		if results == nil {
			continue
		}
		p.addPartialBatch(lib, st, c, m, results, listPath, verdict)
	}
}

func (p *Plan) perItemResults(lib *Library, rpc string, c *RPCContract, m *catalog.Method) *catalog.Field {
	listPath, verdict, ok := strings.Cut(chain.ItemEnvelope(), "[].")
	if !ok || listPath == "" || verdict == "" || chain.IsReadOnlyCall(rpc) {
		return nil
	}
	var results *catalog.Field
	for _, f := range catalog.DescribeMessage(m.Output()).Fields {
		if f.Name == listPath && f.Repeated && f.Kind == "message" {
			results = f
		}
	}
	if results == nil {
		return nil
	}
	if c.Effects.perItem() || perItemFailure().MatchString(c.Summary) || slices.ContainsFunc(lib.AllFailures(rpc), func(f Failure) bool { return perItemFailure().MatchString(f.When) }) {
		return results
	}
	return nil
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
			partial, reads := p.batchProbe(lib, st, m, key, results, listPath, verdict, "partial",
				fmt.Sprintf("%s.1 has %s %d and is refused with %s on that line only; the lines around it are applied.", rf.Name, sub.Name, min-1, failure.Label()),
				[]batchLine{{item: cloneBody(first)}, {item: bad(last), refused: failure}, {item: cloneBody(last)}})
			p.note("step %s: %s sends three %s with the middle one refused (%s %d, %s declared per line); it asserts each line's own "+
				"verdict, and %s assert that what each applied line reports is what is stored, so a batch that stops at the refused line, "+
				"applies it, or reports stale values for later lines fails", st.ID, partial.ID, rf.Name, sub.Name, min-1, failure.Label(), stepIDList(reads))
			return
		}
	}
	p.gap("step %s: no numeric field of a repeated request item has a minimum in a failure's when:, so no batch "+
		"with a refused middle item was planned: write one by hand", st.ID)
}

func (p *Plan) batchProbe(lib *Library, st *chain.Step, m *catalog.Method, key string, results *catalog.Field, listPath, verdict, suffix, desc string,
	lines []batchLine) (*chain.Step, []*chain.Step) {
	probe := probeStep(st, p.freeStepID(st.ID+"_"+suffix))
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
			e, ok := p.readerMatching(lib, prod, ref[1], true)
			if !ok {
				continue
			}
			base := p.readBase(e)
			read := e.readStep(p.freeStepID(base+"_before_"+probe.ID),
				fmt.Sprintf("the %s as it stands before %s, whose line naming it is refused.", e.carrier, probe.ID), "${"+prod.ID+"."+e.idPath+"}")
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
				e, ok := p.readerMatching(lib, prod, ref[1], true)
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
		read := e.readStep(p.freeStepID(p.readBase(e)+"_after_"+partial.ID), "", "${"+e.producer.ID+"."+e.idPath+"}")
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
