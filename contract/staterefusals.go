package contract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var (
	notFoundReason = regexp.MustCompile(`NotFound|Unknown|NoSuch|DoesNotExist|Missing[A-Z]`)
	notFoundWhen   = regexp.MustCompile(`(?i)\bno \w+ (?:has|with|matches|named)\b|\bunknown\b|\bdoes not exist\b|\bnot found\b|\bnames no\b|\bno such\b`)
)

type stateEntity struct {
	field    string
	producer *chain.Step
	idPath   string
	carrier  string
	itemMsg  string
	idField  string
	state    *catalog.Field
}

func (p *Plan) entityStates(lib *Library, st *chain.Step, c *RPCContract) []stateEntity {
	out := []stateEntity{}
	for _, name := range sortedFieldNames(c.Fields) {
		f := c.Fields[name]
		if f == nil || f.From == "" || strings.Contains(name, ".") {
			continue
		}
		ref, err := ParseRef(f.From)
		if err != nil {
			continue
		}
		key, ok := namecase.LookupKey(st.Body, name)
		if !ok {
			continue
		}
		text, _ := st.Body[key].(string)
		src, isRef := refSource(text)
		if !isRef {
			continue
		}
		prod := p.stepByID(src)
		if prod == nil || chain.IsReadOnlyCall(prod.Call) || canonicalCall(p.cat, prod.Call) != canonicalCall(p.cat, ref.RPC) {
			continue
		}
		pm, err := p.cat.Lookup(prod.Call)
		if err != nil {
			continue
		}
		segs := chain.SplitPath(ref.Path)
		if len(segs) != 2 {
			continue
		}
		for _, fd := range catalog.DescribeMessage(pm.Output()).Fields {
			if fd.Name != segs[0] || fd.Kind != "message" || fd.Repeated {
				continue
			}
			for _, sf := range fd.Fields {
				if len(sf.EnumValues) > 1 && !sf.Repeated {
					out = append(out, stateEntity{field: key, producer: prod, idPath: ref.Path, carrier: fd.Name, itemMsg: fd.Message, idField: segs[1], state: sf})
					break
				}
			}
		}
	}
	return out
}

func failureState(f Failure, values []string, short map[string]string) string {
	for _, v := range values {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(short[v]) + `\b`)
		if re.MatchString(f.When) {
			return v
		}
	}
	words := strings.ToLower(strings.Join(namecase.Words(f.Reason), " "))
	for _, v := range values {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(strings.ToLower(short[v])) + `\b`)
		if words != "" && re.MatchString(words) {
			return v
		}
	}
	return ""
}

func probeable(f Failure) bool {
	return f.Unreachable == "" && (f.Code != 0 || f.Reason != "" || f.ConnectCode != "") && !isUnauthenticated(f) && !perItemFailure.MatchString(f.When)
}

func (p *Plan) probeStateRefusals(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || p.isLogin(st.Call) {
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
			if pc, ok := lib.Get(e.producer.Call); ok {
				initial = stateIn([]string{pc.Exports[e.carrier], pc.Summary}, values, short)
			}
			var transitions []transition
			for _, f := range lib.AllFailures(st.Call) {
				if !probeable(f) {
					continue
				}
				state := failureState(f, values, short)
				if state == "" || state == initial {
					continue
				}
				if transitions == nil {
					t := &listTarget{step: st, itemMsg: e.itemMsg, itemID: e.idField, carrier: e.carrier}
					transitions = p.transitionsFor(lib, t, e.producer, values, short, initial)
				}
				var move *transition
				for i := range transitions {
					if transitions[i].value == state {
						move = &transitions[i]
					}
				}
				if move == nil {
					p.note("step %s: its contract declares %s for a %s in %s, but no write rpc in the contracts says it moves "+
						"a %s there (a summary or export naming %s), so no fixture is put in that state to probe it",
						st.ID, f.Label(), e.carrier, short[state], e.carrier, short[state])
					continue
				}
				p.addStateRefusal(lib, st, m, e, f, *move, short)
			}
		}
	}
}

func (p *Plan) addStateRefusal(lib *Library, st *chain.Step, m *catalog.Method, e stateEntity, f Failure, move transition, short map[string]string) {
	label := strings.ToLower(short[move.value])
	fixture := copyStep(e.producer, p.freeStepID(e.producer.ID+"_for_"+st.ID+"_"+label))
	fixture.Export = nil
	p.freshen(lib, fixture)
	renameStepRefs(fixture, e.producer.ID, fixture.ID)
	fixture.Description = fmt.Sprintf("as %s, a %s of its own for %s to find %s.", e.producer.ID, e.carrier, st.ID, short[move.value])
	p.assertEcho(fixture)
	fixtureID := "${" + fixture.ID + "." + e.idPath + "}"

	body := catalog.ScaffoldWith(move.method.Input(), catalog.ScaffoldOptions{})
	setBodyPath(body, move.field, fixtureID)
	moved := &chain.Step{
		ID:          p.freeStepID(defaultID(move.method.Name) + "_to_" + label + "_for_" + st.ID),
		Description: fmt.Sprintf("moves %s to %s, the state in which %s must be refused.", fixture.ID, short[move.value], st.ID),
		Call:        move.method.FullName,
		Auth:        move.contract.Auth,
		Body:        body,
		Expect:      append(SuccessExpectation(move.method), chain.Expectation{Path: e.carrier + "." + e.state.Name, Equals: move.value}),
	}

	refused := copyStep(st, p.freeStepID(st.ID+"_when_"+label))
	refused.Export = nil
	if !chain.IsReadOnlyCall(st.Call) {
		p.freshen(lib, refused)
	}
	setBodyPath(refused.Body, e.field, fixtureID)
	refused.Expect = refusalOf(m, f)
	refused.Description = fmt.Sprintf("on %s already %s: refused with %s (%s), and nothing it would have changed moves.",
		withArticle(e.carrier), short[move.value], f.Label(), strings.TrimSpace(f.When))
	p.Chain.Steps = append(p.Chain.Steps, fixture, moved)
	p.Chain.Steps = append(p.Chain.Steps, p.guardUnchanged(lib, []*chain.Step{refused}, refused.ID)...)
	p.note("step %s: its contract declares %s for a %s in %s, so %s moves a fresh %s there with %s and %s expects exactly "+
		"%s, the reads around it asserting the %s and what it holds unchanged: a backend that answers another code, or "+
		"acts on the %s anyway, fails", st.ID, f.Label(), e.carrier, short[move.value], fixture.ID, e.carrier, moved.ID,
		refused.ID, f.Label(), e.carrier, e.carrier)
}

func (p *Plan) probeLookupRefusals(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || p.isLogin(st.Call) {
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
		failures := []Failure{}
		for _, f := range lib.AllFailures(st.Call) {
			if probeable(f) && f.Code != 0 && (notFoundReason.MatchString(f.Reason) || notFoundWhen.MatchString(f.When)) {
				failures = append(failures, f)
			}
		}
		if len(failures) == 0 {
			continue
		}
		lookups := []string{}
		for _, name := range sortedFieldNames(c.Fields) {
			if f := c.Fields[name]; f != nil && f.From != "" {
				lookups = append(lookups, name)
			}
		}
		for _, name := range lookups {
			ref, err := ParseRef(c.Fields[name].From)
			if err != nil {
				continue
			}
			f, found := lookupFailure(failures, name, ref, len(lookups) == 1)
			if !found {
				continue
			}
			p.addLookupRefusal(lib, st, m, name, f)
		}
	}
}

func lookupFailure(failures []Failure, field string, ref Ref, only bool) (Failure, bool) {
	for _, f := range failures {
		if f.Field != "" && stripIndexes(f.Field) == stripIndexes(field) {
			return f, true
		}
	}
	noun := namecase.Fold(chain.SplitPath(ref.Path)[0])
	for _, f := range failures {
		if f.Field != "" {
			continue
		}
		if noun != "" && (strings.Contains(namecase.Fold(f.Reason), noun) || regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(noun)+`\b`).MatchString(f.When)) {
			return f, true
		}
	}
	if only && len(failures) == 1 && failures[0].Field == "" {
		return failures[0], true
	}
	return Failure{}, false
}

func (p *Plan) addLookupRefusal(lib *Library, st *chain.Step, m *catalog.Method, field string, f Failure) {
	segs := chain.SplitPath(field)
	path := field
	if len(segs) > 1 {
		key, ok := namecase.LookupKey(st.Body, segs[0])
		list, isList := st.Body[key].([]any)
		if !ok || !isList || len(list) == 0 {
			return
		}
		path = fmt.Sprintf("%s.%d.%s", key, len(list)-1, strings.Join(segs[1:], "."))
	}
	cur, ok := bodyValue(st.Body, path)
	if !ok {
		return
	}
	if _, isText := cur.(string); !isText {
		return
	}
	probe := copyStep(st, p.freeStepID(st.ID+"_unknown_"+leafName(field)))
	probe.Export = nil
	if !chain.IsReadOnlyCall(st.Call) {
		p.freshen(lib, probe)
	}
	unknown := "no-such-" + strings.ReplaceAll(leafName(field), "_", "-")
	setBodyPath(probe.Body, path, unknown)
	probe.Expect = refusalOf(m, f)
	probe.Description = fmt.Sprintf("%s names nothing that exists: refused with %s (%s).", path, f.Label(), strings.TrimSpace(f.When))
	steps := []*chain.Step{probe}
	if !chain.IsReadOnlyCall(st.Call) && len(referencedSteps(probe.Body)) > 0 {
		steps = p.guardUnchanged(lib, steps, probe.ID)
	}
	p.Chain.Steps = append(p.Chain.Steps, steps...)
	p.note("step %s: %s sends %s = %q, an id nothing created, and expects exactly %s: a backend that answers another code, "+
		"or accepts the reference, fails", st.ID, probe.ID, path, unknown, f.Label())
}

func refusalOf(m *catalog.Method, f Failure) []chain.Expectation {
	if f.ConnectCode != "" && f.Code == 0 {
		return refusalFor(m, f)
	}
	return withoutAbsentCarrier(refusalExpectations(m, f))
}

func withArticle(noun string) string {
	if noun != "" && strings.ContainsRune("aeiouAEIOU", rune(noun[0])) {
		return "an " + noun
	}
	return "a " + noun
}
