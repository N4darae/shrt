package contract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/N4darae/shrt/chain"
)

var (
	restoreWord  = lazyRegexp(`(?i)\b(?:return(?:s|ed|ing)?|(?:gives?|gave|given|giving) (?:\w+ ){0,2}back|restor(?:es|ed|ing|e)|releas(?:es|ed|ing|e)|refund(?:s|ed|ing)?|(?:puts?|putting) (?:\w+ ){0,2}back|restock(?:s|ed|ing)?)\b`)
	clauseBreaks = lazyRegexp(`[.;]\s*`)
)

func namingClause(texts []string, word string, kind *regexp.Regexp) string {
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(word) + `\b`)
	for _, text := range texts {
		for _, clause := range clauseBreaks().Split(text, -1) {
			if re.MatchString(clause) && kind.MatchString(clause) {
				return clause
			}
		}
	}
	return ""
}

func (p *Plan) probeComposedTransitions(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || chain.IsReadOnlyCall(st.Call) || p.isLogin(st.Call) {
			continue
		}
		c, m, ok := p.contractOf(lib, st.Call)
		if !ok {
			continue
		}
		for _, e := range p.entityStates(st, c) {
			values := e.state.EnumValues[1:]
			short := enumShort(e.state.EnumValues)
			skipped, skips := p.skippedStart(lib, st, c, e)
			initial := ""
			pc, ok := lib.Get(e.producer.Call)
			if ok {
				initial = stateIn([]string{pc.Exports[e.carrier], pc.Summary}, values, short)
			}
			held := ""
			if ok && initial != "" && c.Effects.restores(short[initial]) {
				for _, b := range p.entityStates(e.producer, pc) {
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
			refused := p.refusedStates(lib, st.Call, values, short)
			t := &listTarget{step: st, itemMsg: e.itemMsg, itemID: e.idField, carrier: e.carrier}
			moves, _ := p.transitionsFor(lib, t, e.producer, values, short, initial)
			for _, tr := range moves {
				if tr.method.FullName == m.FullName || tr.value == result || refused[tr.value] || (held != "" && tr.value != held) {
					continue
				}
				texts := []string{c.Summary, c.Exports[e.carrier], lib.DescriptionOf(lib.Domain(m.FullName))}
				if p.addComposedTransition(lib, st, e, tr, result, short, namingClause(texts, short[tr.value], restoreWord()) != "" || c.Effects.restores(short[tr.value])) {
					for field, ef := range c.Effects {
						if ef != nil && ef.Restore != "" && SameState(ef.Restore, short[tr.value]) {
							p.met[[2]string{st.Call, field}] = true
						}
					}
				}
			}
			if skips && held == "" {
				p.addSkippedStart(lib, st, e, skipped, result, short, !c.Effects.any(func(ef *Effect) bool { return ef.Restore == "" && ef.Is != EffectNone }))
			}
		}
	}
}

type skippedStart struct {
	need, carrier, state, reached string
}

func (p *Plan) skippedStart(lib *Library, st *chain.Step, c *RPCContract, e stateEntity) (skippedStart, bool) {
	values := e.state.EnumValues[1:]
	short := enumShort(e.state.EnumValues)
	pc, ok := lib.Get(e.producer.Call)
	if !ok {
		return skippedStart{}, false
	}
	initial := stateIn([]string{pc.Exports[e.carrier], pc.Summary}, values, short)
	if initial == "" || initial == stateIn([]string{c.Exports[e.carrier], c.Summary}, values, short) || p.refusedStates(lib, st.Call, values, short)[initial] {
		return skippedStart{}, false
	}
	for _, need := range c.Needs {
		rpc, _ := SplitNode(need)
		rpc = canonicalCall(p.cat, rpc)
		nc, ok := lib.Get(rpc)
		if !ok || chain.IsReadOnlyCall(rpc) {
			continue
		}
		if _, path := entityField(p.cat, nc, e.producer); path != e.idPath {
			continue
		}
		if reached := stateIn([]string{nc.Exports[e.carrier], nc.Summary}, values, short); reached != "" && reached != initial && c.Effects.restores(short[reached]) {
			return skippedStart{need: rpc, carrier: e.carrier, state: short[initial], reached: short[reached]}, true
		}
	}
	return skippedStart{}, false
}

func (sk skippedStart) why() string {
	return fmt.Sprintf("needs: [%s] only takes the %s to %s, where its restore: applies, so it acts on %s too", shortRPC(sk.need), sk.carrier, sk.reached, withArticle(sk.state+" "+sk.carrier))
}

func (p *Plan) refusedStates(lib *Library, call string, values []string, short map[string]string) map[string]bool {
	out := map[string]bool{}
	for _, f := range lib.AllFailures(call) {
		if probeable(f) {
			out[failureState(f, values, short)] = true
		}
	}
	return out
}

func (p *Plan) actOnFixture(lib *Library, st *chain.Step, e stateEntity, id, purpose string) (*chain.Step, *chain.Step) {
	fid := p.freeStepID(e.producer.ID + "_for_" + id)
	fixture := p.fixtureCopy(lib, e.producer, fid, map[string]string{e.producer.ID: fid}, fmt.Sprintf("as %s, %s of its own %s.", e.producer.ID, withArticle(e.carrier), purpose))
	act := probeStep(st, id)
	p.freshen(lib, act)
	setBodyPath(act.Body, e.field, "${"+fid+"."+e.idPath+"}")
	renameStepRefs(act, st.ID, act.ID)
	act.Expect = retargetExpect(act.Expect, e.producer.ID, fid)
	return fixture, act
}

func (p *Plan) startedCopy(lib *Library, act *chain.Step, e stateEntity, sk skippedStart) (*chain.Step, *chain.Step) {
	id := p.freeStepID(act.ID + "_from_" + strings.ToLower(sk.state))
	fixture, again := p.actOnFixture(lib, act, e, id, fmt.Sprintf("left %s for %s to act on", sk.state, id))
	again.Description = fmt.Sprintf("as %s, on %s still %s: %s.", act.ID, withArticle(e.carrier), sk.state, sk.why())
	if p.unneeded == nil {
		p.unneeded = map[*chain.Step]string{}
	}
	p.unneeded[again] = sk.need
	return fixture, again
}

func (p *Plan) fromSkippedStart(lib *Library, act *chain.Step) (*chain.Step, *chain.Step, string) {
	c, _, ok := p.contractOf(lib, act.Call)
	if !ok {
		return nil, nil, ""
	}
	for _, e := range p.entityStates(act, c) {
		if sk, ok := p.skippedStart(lib, act, c, e); ok {
			fixture, again := p.startedCopy(lib, act, e, sk)
			return fixture, again, fmt.Sprintf("a fresh %s still %s, with no %s first: %s", e.carrier, sk.state, shortRPC(sk.need), sk.why())
		}
	}
	return nil, nil, ""
}

func (p *Plan) addSkippedStart(lib *Library, st *chain.Step, e stateEntity, sk skippedStart, result string, short map[string]string, restores bool) {
	fixture, act := p.startedCopy(lib, st, e, sk)
	held, _ := p.readAround(lib, fixture, act, nil, e, "", result, short, restores)
	what := fmt.Sprintf("the %s read after it is %s", e.carrier, short[result])
	if len(held) > 0 {
		what += fmt.Sprintf(", and %s assert what it holds is unchanged, as nothing took it", strings.Join(held, ", "))
	}
	p.note("step %s: %s; %s acts on a fresh %s still %s, with no %s first: %s", st.ID, sk.why(), act.ID, e.carrier, sk.state, shortRPC(sk.need), what)
}

func (p *Plan) addComposedTransition(lib *Library, st *chain.Step, e stateEntity, tr transition, result string, short map[string]string, restores bool) bool {
	label := strings.ToLower(short[tr.value])
	id := p.freeStepID(st.ID + "_after_" + label)
	fixture, act := p.actOnFixture(lib, st, e, id, fmt.Sprintf("for %s to move to %s and %s to act on", defaultID(tr.method.Name), short[tr.value], st.ID))
	moved := tr.step(p.freeStepID(defaultID(tr.method.Name)+"_before_"+id),
		fmt.Sprintf("moves %s to %s, the state %s then starts from.", fixture.ID, short[tr.value], id), "${"+fixture.ID+"."+e.idPath+"}", e.carrier+"."+e.state.Name)
	act.Expect = retargetExpect(act.Expect, e.via, moved.ID)
	act.Description = fmt.Sprintf("%s on %s that is %s: it must still reach %s.", st.ID, withArticle(e.carrier), short[tr.value], short[result])
	held, read := p.readAround(lib, fixture, act, moved, e, tr.value, result, short, restores)
	what, gap := fmt.Sprintf("the %s read after it is %s", e.carrier, short[result]), ""
	if len(held) > 0 {
		what += fmt.Sprintf(", and %s assert what it holds is back to the reads taken before %s, as the contract says for %s",
			strings.Join(held, ", "), moved.ID, withArticle(short[tr.value]+" "+e.carrier))
	} else if read > 1 {
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

func (p *Plan) readAround(lib *Library, fixture, act, moved *chain.Step, e stateEntity, from, result string, short map[string]string, restores bool) ([]string, int) {
	p.Chain.Steps = append(p.Chain.Steps, fixture)
	first := act
	if moved != nil {
		first = moved
	}
	before, after := []*chain.Step{}, []*chain.Step{}
	reserved := map[string]bool{}
	held := []string{}
	entities := p.entitiesOf(lib, act)
	for _, en := range entities {
		base := p.readBase(en)
		what := fmt.Sprintf("the %s before %s acts on %s.", en.carrier, act.ID, fixture.ID)
		if moved != nil {
			what = fmt.Sprintf("the %s before %s moves %s to %s.", en.carrier, moved.ID, fixture.ID, short[from])
		}
		read := en.readStep(p.freeProbeID(base+"_before_"+first.ID, reserved), what, "${"+en.producer.ID+"."+en.idPath+"}")
		check := copyStep(read, p.freeProbeID(base+"_after_"+act.ID, reserved))
		switch {
		case en.producer == fixture:
			check.Description = fmt.Sprintf("the %s after %s is %s.", en.carrier, act.ID, short[result])
			check.Expect = append(check.Expect, chain.Expectation{Path: en.carrier + "." + e.state.Name, Equals: result})
		case restores && moved == nil:
			check.Description = fmt.Sprintf("the %s after %s holds what it held before it: %s read as in %s, since %s gives back only "+
				"what was taken and nothing took it.", en.carrier, act.ID, strings.Join(en.scalars, ", "), read.ID, shortRPC(act.Call))
			check.Expect = append(check.Expect, en.asIn(read.ID)...)
			held = append(held, check.ID)
		case restores:
			check.Description = fmt.Sprintf("the %s after %s holds what it held before %s: %s read as in %s, since the contract says "+
				"%s on %s gives it back.", en.carrier, act.ID, moved.ID, strings.Join(en.scalars, ", "), read.ID, shortRPC(act.Call),
				withArticle(short[from]+" "+e.carrier))
			check.Expect = append(check.Expect, en.asIn(read.ID)...)
			held = append(held, check.ID)
		default:
			check.Description = fmt.Sprintf("the %s after %s, as it stands.", en.carrier, act.ID)
		}
		before = append(before, read)
		after = append(after, check)
	}
	p.Chain.Steps = append(p.Chain.Steps, before...)
	if moved != nil {
		p.Chain.Steps = append(p.Chain.Steps, moved)
	}
	p.Chain.Steps = append(p.Chain.Steps, act)
	p.Chain.Steps = append(p.Chain.Steps, after...)
	return held, len(entities)
}

func retargetExpect(expect []chain.Expectation, from, to string) []chain.Expectation {
	out := make([]chain.Expectation, len(expect))
	for i, e := range expect {
		out[i] = e.MapOperands(func(v any) any { return rewriteRefs(v, map[string]string{from: to}) })
	}
	return out
}
