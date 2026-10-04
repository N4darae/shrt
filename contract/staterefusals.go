package contract

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

type stateEntity struct {
	field    string
	producer *chain.Step
	idPath   string
	carrier  string
	itemMsg  string
	idField  string
	state    *catalog.Field
	via      string
}

func (p *Plan) entityStates(st *chain.Step, c *RPCContract) []stateEntity {
	out := []stateEntity{}
	for _, name := range chain.SortedKeys(c.Fields) {
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
		fd := fieldByName(catalog.DescribeMessage(pm.Output()).Fields, segs[0])
		if fd == nil || fd.Kind != "message" || fd.Repeated {
			continue
		}
		if i := slices.IndexFunc(fd.Fields, func(sf *catalog.Field) bool { return len(sf.EnumValues) > 1 && !sf.Repeated }); i >= 0 {
			out = append(out, stateEntity{field: key, producer: prod, idPath: ref.Path, carrier: fd.Name, itemMsg: fd.Message, idField: segs[1], state: fd.Fields[i]})
		}
	}
	return out
}

func failureState(f Failure, values []string, short map[string]string) string {
	for _, v := range values {
		if mentionsField(f.When, short[v]) {
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
	return f.Unreachable == "" && (f.Code != 0 || f.Reason != "" || f.ConnectCode != "") && !isUnauthenticated(f) && !perItemFailure().MatchString(f.When)
}

func (p *Plan) probeStateRefusals(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || p.isLogin(st.Call) {
			continue
		}
		c, m, ok := p.contractOf(lib, st.Call)
		if !ok {
			continue
		}
		for _, e := range p.entityStates(st, c) {
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
					transitions, _ = p.transitionsFor(lib, t, e.producer, values, short, initial)
				}
				at := slices.IndexFunc(transitions, func(tr transition) bool { return tr.value == state })
				if at < 0 {
					p.note("step %s: its contract declares %s for %s in %s, but no write rpc in the contracts says it moves "+
						"%s there (a summary or export naming %s), so no fixture is put in that state to probe it",
						st.ID, f.Label(), withArticle(e.carrier), short[state], withArticle(e.carrier), short[state])
					continue
				}
				p.addStateRefusal(lib, st, m, e, f, transitions[at], short)
			}
		}
	}
}

func (p *Plan) addStateRefusal(lib *Library, st *chain.Step, m *catalog.Method, e stateEntity, f Failure, move transition, short map[string]string) {
	label := strings.ToLower(short[move.value])
	fid := p.freeStepID(e.producer.ID + "_for_" + st.ID + "_" + label)
	fixture := p.fixtureCopy(lib, e.producer, fid, map[string]string{e.producer.ID: fid}, fmt.Sprintf("as %s, a %s of its own for %s to find %s.", e.producer.ID, e.carrier, st.ID, short[move.value]))
	fixtureID := "${" + fixture.ID + "." + e.idPath + "}"

	moved := move.step(p.freeStepID(chain.SnakeCase(move.method.Name)+"_to_"+label+"_for_"+st.ID),
		fmt.Sprintf("moves %s to %s, the state in which %s must be refused.", fixture.ID, short[move.value], st.ID), fixtureID, e.carrier+"."+e.state.Name)

	refused := p.probeCopy(lib, st, "when_"+label)
	setBodyPath(refused.Body, e.field, fixtureID)
	refused.Expect = refusalFor(m, f, false)
	refused.Description = fmt.Sprintf("on %s already %s: refused with %s (%s), and nothing it would have changed moves.",
		withArticle(e.carrier), short[move.value], f.Label(), strings.TrimSpace(f.When))
	p.Chain.Steps = append(p.Chain.Steps, fixture, moved)
	p.Chain.Steps = append(p.Chain.Steps, p.guardUnchanged(lib, []*chain.Step{refused}, refused.ID)...)
	p.note("step %s: its contract declares %s for %s in %s, so %s moves a fresh %s there with %s and %s expects exactly "+
		"%s, the reads around it asserting the %s and what it holds unchanged: a backend that answers another code, or "+
		"acts on the %s anyway, fails", st.ID, f.Label(), withArticle(e.carrier), short[move.value], fixture.ID, e.carrier, moved.ID,
		refused.ID, f.Label(), e.carrier, e.carrier)
}

func withArticle(noun string) string {
	if noun != "" && strings.ContainsRune("aeiouAEIOU", rune(noun[0])) {
		return "an " + noun
	}
	return "a " + noun
}
