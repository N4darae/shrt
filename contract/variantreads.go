package contract

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

func (p *Plan) readBackVariants(lib *Library) {
	targets := map[string]bool{}
	for _, node := range p.Targets {
		rpc, _ := SplitNode(node)
		targets[rpc] = true
	}
	mains := map[string]bool{}
	for _, id := range p.stepOf {
		mains[id] = true
	}
	added := []string{}
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !targets[canonicalCall(p.cat, st.Call)] || !p.succeedsAsWrite(st) {
			continue
		}
		if !mains[st.ID] && (p.actedOnLater(st) || p.groupOf[st] == "replay") {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		car := singleCarrier(m)
		if car == nil {
			continue
		}
		e, ok := p.ownRecordRead(lib, st, m, car)
		if !ok {
			continue
		}
		states := []chain.Expectation{}
		for _, x := range st.Expect {
			sub := fieldByName(car.Fields, strings.TrimPrefix(x.Path, car.Name+"."))
			text, literal := x.Equals.(string)
			if sub == nil || len(sub.EnumValues) < 2 || !literal || strings.Contains(text, "${") {
				continue
			}
			if rf := fieldByName(carrierFields(e.reader, e.carrier), sub.Name); rf != nil {
				states = append(states, chain.Expectation{Path: e.carrier + "." + sub.Name, Equals: text})
			}
		}
		if e.producer == st && p.replaysAnother(st, e.idPath) {
			continue
		}
		ref := "${" + e.producer.ID + "." + e.idPath + "}"
		if r := p.readRightAfter(st, e, ref); r != nil {
			for _, x := range states {
				if !hasExpectOn(r, x.Path) {
					r.Expect = append(r.Expect, x)
				}
			}
			continue
		}
		if len(states) == 0 && (e.producer != st || mains[st.ID] || p.readsCreated(st, e.idPath)) {
			continue
		}
		body := catalog.ScaffoldWith(e.reader.Input(), catalog.ScaffoldOptions{})
		setBodyPath(body, e.field, ref)
		r := &chain.Step{
			ID:          p.freeStepID(defaultID(e.reader.Name) + "_after_" + st.ID),
			Description: fmt.Sprintf("the %s as %s left it, read back as its main step's is.", e.carrier, st.ID),
			Call:        e.reader.FullName,
			Auth:        e.contract.Auth,
			Body:        body,
			Expect:      SuccessExpectation(e.reader),
		}
		p.assertEcho(r)
		r.Expect = append(r.Expect, states...)
		p.insertAfter(st.ID, r)
		added = append(added, r.ID)
	}
	if len(added) > 0 {
		p.note("%s %s the record back after a variant of the target that succeeds, as the main step's is read, so a variant "+
			"the backend answers correctly but stores differently fails", strings.Join(clipList(added, 6), ", "),
			pluralVerb(len(added), "reads", "read"))
	}
}

func (p *Plan) succeedsAsWrite(st *chain.Step) bool {
	return !chain.IsReadOnlyCall(st.Call) && !st.AllowFail && !st.SkipAuth && st.Auth != invalidProfile &&
		!isRefusalStep(st) && effectOutcome(st) == outcomeSuccess && !p.streams(st) && !p.isLogin(st.Call)
}

func (p *Plan) actedOnLater(st *chain.Step) bool {
	at := stepIndex(p.Chain.Steps, st.ID)
	for _, s := range p.Chain.Steps[at+1:] {
		if chain.IsReadOnlyCall(s.Call) {
			continue
		}
		if containsString(referencedSteps(s.Body), st.ID) {
			return true
		}
	}
	return false
}

func (p *Plan) ownRecordRead(lib *Library, st *chain.Step, m *catalog.Method, car *catalog.Field) (entityRead, bool) {
	for _, e := range p.entitiesOf(lib, st) {
		for _, f := range catalog.DescribeMessage(e.reader.Output()).Fields {
			if f.Name == e.carrier && f.Message == car.Message {
				return e, true
			}
		}
	}
	if idPath := p.createdIDPath(st, m); idPath != "" {
		return p.readerMatching(lib, st, idPath, false)
	}
	return entityRead{}, false
}

func (p *Plan) readRightAfter(st *chain.Step, e entityRead, ref string) *chain.Step {
	at := stepIndex(p.Chain.Steps, st.ID)
	for _, s := range p.Chain.Steps[at+1:] {
		if !chain.IsReadOnlyCall(s.Call) {
			return nil
		}
		if canonicalCall(p.cat, s.Call) != e.reader.FullName || isRefusalStep(s) {
			continue
		}
		if v, ok := bodyValue(s.Body, e.field); ok && v == ref {
			return s
		}
	}
	return nil
}
