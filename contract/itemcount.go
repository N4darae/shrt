package contract

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

const manyItems = 3

func (p *Plan) recordPreparation(clone, original string) {
	if p.preps == nil {
		p.preps = map[string][]string{}
	}
	if !containsString(p.preps[clone], original) {
		p.preps[clone] = append(p.preps[clone], original)
	}
}

type itemList struct {
	fixture *chain.Step
	key     string
	field   *catalog.Field
}

func (p *Plan) itemListOf(st *chain.Step) (itemList, bool) {
	m, err := p.cat.Lookup(st.Call)
	if err != nil {
		return itemList{}, false
	}
	for _, f := range catalog.DescribeMessage(m.Input()).Fields {
		if !f.Repeated || f.Kind != "message" || f.MapKey != "" || f.JSONForm != "" || len(f.Fields) == 0 {
			continue
		}
		key, ok := namecase.LookupKey(st.Body, f.Name)
		if !ok {
			continue
		}
		if list, isList := st.Body[key].([]any); isList && len(list) == 2 {
			return itemList{fixture: st, key: key, field: f}, true
		}
	}
	return itemList{}, false
}

func (p *Plan) probeItemCounts(lib *Library, isTarget func(*chain.Step) bool) {
	thirds := map[string]string{}
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || chain.IsReadOnlyCall(st.Call) || p.isLogin(st.Call) {
			continue
		}
		il, ok := p.itemListOf(st)
		if !ok {
			for _, src := range referencedSteps(st.Body) {
				prod := p.stepByID(src)
				if prod == nil || prod == st || chain.IsReadOnlyCall(prod.Call) {
					continue
				}
				if il, ok = p.itemListOf(prod); ok {
					break
				}
			}
		}
		if !ok {
			continue
		}
		p.addItemCounts(lib, st, il, thirds)
	}
}

func (p *Plan) addItemCounts(lib *Library, st *chain.Step, il itemList, thirds map[string]string) {
	fx := il.fixture
	list := fx.Body[il.key].([]any)
	added := []*chain.Step{}

	third := cloneBody(list[1])
	if item, ok := third.(map[string]any); ok {
		distinctItem(item, il.field.Fields)
	}
	for _, sec := range p.seconds {
		if sec.reader != fx.ID || !readsValue(list[1], sec.clone) {
			continue
		}
		id, ok := thirds[sec.src]
		if !ok {
			var made []*chain.Step
			id, made = p.thirdProducer(sec)
			thirds[sec.src] = id
			added = append(added, made...)
		}
		third = rewriteRefs(third, map[string]string{sec.clone: id})
	}

	counts := []struct {
		n     int
		items []any
	}{
		{1, []any{cloneBody(list[0])}},
		{manyItems, []any{cloneBody(list[0]), cloneBody(list[1]), third}},
	}
	ids := []string{}
	for _, c := range counts {
		suffix := fmt.Sprintf("%d_%s", c.n, il.field.Name)
		v := copyStep(fx, p.freeStepID(fx.ID+"_"+suffix))
		v.Export = nil
		v.Body[il.key] = c.items
		p.freshen(lib, v)
		renameStepRefs(v, fx.ID, v.ID)
		v.Description = fmt.Sprintf("as %s, but with %d %s item(s) instead of 2.", fx.ID, c.n, il.field.Name)
		added = append(added, v)
		if fx == st {
			ids = append(ids, v.ID)
			continue
		}
		t := copyStep(st, p.freeStepID(st.ID+"_"+suffix))
		t.Export = nil
		p.freshen(lib, t)
		renameStepRefs(t, fx.ID, v.ID)
		t.Description = fmt.Sprintf("%s on %s, whose %s has %d item(s): the same outcome as on %s's 2.", st.ID, v.ID, il.field.Name, c.n, fx.ID)
		added = append(added, t)
		ids = append(ids, t.ID)
	}
	p.Chain.Steps = append(p.Chain.Steps, added...)
	p.note("step %s: %s sends %s with 2 items, so %s repeat %s with one item and with three, expecting what %s expects: "+
		"a backend whose logic changes with the number of items (a loop bound, a limit, a check on the first or last only) "+
		"passes on 2 and fails on one or three. The third item reads its own resource where the second does",
		st.ID, fx.ID, il.field.Name, strings.Join(ids, " and "), st.ID, st.ID)
}

func readsValue(v any, id string) bool {
	for _, ref := range allStepRefs(v) {
		if ref[0] == id {
			return true
		}
	}
	return false
}

func (p *Plan) thirdProducer(sec producerSecond) (string, []*chain.Step) {
	orig := p.stepByID(sec.src)
	id := p.freeStepID(sec.src)
	clone := copyStep(orig, id)
	suffix := strings.TrimPrefix(id, sec.src+"_")
	if m, err := p.cat.Lookup(orig.Call); err == nil {
		distinctProducerAt(clone.Body, catalog.DescribeMessage(m.Input()).Fields, suffix, 2)
	}
	renameStepRefs(clone, sec.src, id)
	clone.Description = fmt.Sprintf("a third %s with its own values, for the third item.", shortRPC(orig.Call))
	made := []*chain.Step{clone}
	for _, prep := range p.preps[sec.clone] {
		src := p.stepByID(prep)
		if src == nil {
			continue
		}
		cid := p.freeStepID(prep)
		c := copyStep(src, cid)
		to := map[string]string{sec.src: id}
		c.Body, _ = rewriteRefs(c.Body, to).(map[string]any)
		for i := range c.Expect {
			c.Expect[i] = c.Expect[i].MapOperands(func(v any) any { return rewriteRefs(v, to) })
		}
		renameStepRefs(c, prep, cid)
		p.distinctPreparation(c, cid, prep)
		made = append(made, c)
	}
	return id, made
}
