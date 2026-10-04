package contract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

var (
	restoreWord  = regexp.MustCompile(`(?i)\b(?:return(?:s|ed|ing)?|(?:gives?|gave|given|giving) (?:\w+ ){0,2}back|restor(?:es|ed|ing|e)|releas(?:es|ed|ing|e)|refund(?:s|ed|ing)?|(?:puts?|putting) (?:\w+ ){0,2}back|restock(?:s|ed|ing)?)\b`)
	clauseBreaks = regexp.MustCompile(`[.;]\s*`)
)

func restoresFrom(texts []string, state string) bool {
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(state) + `\b`)
	for _, text := range texts {
		for _, clause := range clauseBreaks.Split(text, -1) {
			if re.MatchString(clause) && restoreWord.MatchString(clause) {
				return true
			}
		}
	}
	return false
}

func (p *Plan) probeComposedTransitions(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || chain.IsReadOnlyCall(st.Call) || p.isLogin(st.Call) {
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
		for _, e := range p.entityStates(lib, st, c) {
			values := e.state.EnumValues[1:]
			short := enumShort(e.state.EnumValues)
			initial := ""
			pc, ok := lib.Get(e.producer.Call)
			if ok {
				initial = stateIn([]string{pc.Exports[e.carrier], pc.Summary}, values, short)
			}
			held := ""
			if ok && initial != "" && c.Effects.restores(short[initial]) {
				for _, b := range p.entityStates(lib, e.producer, pc) {
					if bc, known := lib.Get(b.producer.Call); known && held == "" && b.idPath == e.idPath && b.carrier == e.carrier {
						b.field, b.via = e.field, e.producer.ID
						held, e, initial = initial, b, stateIn([]string{bc.Exports[b.carrier], bc.Summary}, values, short)
					}
				}
			}
			result := stateIn([]string{c.Exports[e.carrier], c.Summary}, values, short)
			if result == "" {
				continue
			}
			refused := map[string]bool{}
			for _, f := range lib.AllFailures(st.Call) {
				if probeable(f) {
					refused[failureState(f, values, short)] = true
				}
			}
			t := &listTarget{step: st, itemMsg: e.itemMsg, itemID: e.idField, carrier: e.carrier}
			moves, _ := p.transitionsFor(lib, t, e.producer, values, short, initial)
			for _, tr := range moves {
				if tr.method.FullName == m.FullName || tr.value == result || refused[tr.value] || (held != "" && tr.value != held) {
					continue
				}
				texts := []string{c.Summary, c.Exports[e.carrier], lib.DescriptionOf(lib.Domain(m.FullName))}
				if p.addComposedTransition(lib, st, m, e, tr, result, short, restoresFrom(texts, short[tr.value]) || c.Effects.restores(short[tr.value])) {
					for field, ef := range c.Effects {
						if ef != nil && ef.Restore != "" && SameState(ef.Restore, short[tr.value]) {
							p.met[[2]string{st.Call, field}] = true
						}
					}
				}
			}
		}
	}
}

func (p *Plan) addComposedTransition(lib *Library, st *chain.Step, m *catalog.Method, e stateEntity, tr transition, result string, short map[string]string, restores bool) bool {
	label := strings.ToLower(short[tr.value])
	id := p.freeStepID(st.ID + "_after_" + label)
	fixture := probeStep(e.producer, p.freeStepID(e.producer.ID+"_for_"+id))
	p.freshen(lib, fixture)
	renameStepRefs(fixture, e.producer.ID, fixture.ID)
	fixture.Description = fmt.Sprintf("as %s, %s of its own for %s to move to %s and %s to act on.", e.producer.ID, withArticle(e.carrier), defaultID(tr.method.Name), short[tr.value], st.ID)
	p.assertEcho(fixture)
	fixtureID := "${" + fixture.ID + "." + e.idPath + "}"

	moved := tr.step(p.freeStepID(defaultID(tr.method.Name)+"_before_"+id),
		fmt.Sprintf("moves %s to %s, the state %s then starts from.", fixture.ID, short[tr.value], id), fixtureID, e.carrier+"."+e.state.Name)

	act := probeStep(st, id)
	p.freshen(lib, act)
	setBodyPath(act.Body, e.field, fixtureID)
	renameStepRefs(act, st.ID, act.ID)
	act.Expect = retargetExpect(retargetExpect(act.Expect, e.producer.ID, fixture.ID), e.via, moved.ID)
	act.Description = fmt.Sprintf("%s on %s that is %s: it must still reach %s.", st.ID, withArticle(e.carrier), short[tr.value], short[result])

	p.Chain.Steps = append(p.Chain.Steps, fixture)
	entities := p.entitiesOf(lib, act)
	before, after := []*chain.Step{}, []*chain.Step{}
	reserved := map[string]bool{}
	held := []string{}
	for _, en := range entities {
		base := p.readBase(en)
		read := en.readStep(p.freeProbeID(base+"_before_"+moved.ID, reserved),
			fmt.Sprintf("the %s before %s moves %s to %s.", en.carrier, moved.ID, fixture.ID, short[tr.value]), "${"+en.producer.ID+"."+en.idPath+"}")
		check := copyStep(read, p.freeProbeID(base+"_after_"+id, reserved))
		if en.producer == fixture {
			check.Description = fmt.Sprintf("the %s after %s is %s.", en.carrier, id, short[result])
			check.Expect = append(check.Expect, chain.Expectation{Path: en.carrier + "." + e.state.Name, Equals: result})
		} else if restores {
			check.Description = fmt.Sprintf("the %s after %s holds what it held before %s: %s read as in %s, since the contract says "+
				"%s on %s gives it back.", en.carrier, id, moved.ID, strings.Join(en.scalars, ", "), read.ID, shortRPC(st.Call),
				withArticle(short[tr.value]+" "+e.carrier))
			for _, name := range en.scalars {
				path := en.carrier + "." + name
				check.Expect = append(check.Expect, chain.Expectation{Path: path, Equals: "${" + read.ID + "." + path + "}"})
			}
			held = append(held, check.ID)
		} else {
			check.Description = fmt.Sprintf("the %s after %s, as it stands.", en.carrier, id)
		}
		before = append(before, read)
		after = append(after, check)
	}
	p.Chain.Steps = append(p.Chain.Steps, before...)
	p.Chain.Steps = append(p.Chain.Steps, moved, act)
	p.Chain.Steps = append(p.Chain.Steps, after...)
	what, gap := fmt.Sprintf("the %s read after it is %s", e.carrier, short[result]), ""
	if len(held) > 0 {
		what += fmt.Sprintf(", and %s assert what it holds is back to the reads taken before %s, as the contract says for %s",
			strings.Join(held, ", "), moved.ID, withArticle(short[tr.value]+" "+e.carrier))
	} else if len(entities) > 1 {
		number := "<number>"
		if r := p.effectRules(lib); len(r.byEntity) > 0 {
			number = r.byEntity[sortedKeys(r.byEntity)[0]].moved
		}
		gap = fmt.Sprintf("%s says nothing of what it gives back from %s (add effects: {%s: {restore: %s}} if it does), so the other reads assert only that they answer",
			shortRPC(st.Call), short[tr.value], number, short[tr.value])
		what += "; " + gap
	}
	note := fmt.Sprintf("step %s: %s moves a fresh %s to %s first and %s then acts on it: %s", st.ID, moved.ID, e.carrier, short[tr.value], id, what)
	if gap == "" {
		p.note("%s", note)
		return len(held) > 0
	}
	p.gapIn(note, "step "+st.ID+": "+gap)
	return false
}

func retargetExpect(expect []chain.Expectation, from, to string) []chain.Expectation {
	out := make([]chain.Expectation, len(expect))
	for i, e := range expect {
		out[i] = e.MapOperands(func(v any) any { return rewriteRefs(v, map[string]string{from: to}) })
	}
	return out
}
