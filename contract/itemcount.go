package contract

import (
	"fmt"
	"strconv"
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
		v.Description = fmt.Sprintf("as %s, but %s holds %s instead of 2.", fx.ID, il.field.Name, itemCount(c.n))
		p.assertEcho(v)
		added = append(added, v)
		if fx == st {
			ids = append(ids, v.ID)
			continue
		}
		t := copyStep(st, p.freeStepID(st.ID+"_"+suffix))
		t.Export = nil
		p.freshen(lib, t)
		renameStepRefs(t, fx.ID, v.ID)
		t.Description = fmt.Sprintf("%s on %s, whose %s holds %s: the same outcome as with %s's 2.", st.ID, v.ID, il.field.Name, itemCount(c.n), fx.ID)
		p.assertEcho(t)
		added = append(added, t)
		ids = append(ids, t.ID)
	}
	p.Chain.Steps = append(p.Chain.Steps, added...)
	many := ""
	if fx == st {
		many = fmt.Sprintf("; %s sends %d, each item with its own resource where the item names one, and asserts the last "+
			"item was applied too, so a cap on the number of items shows", p.manyItems(lib, st, il, third, thirds), lotsOfItems)
	}
	p.note("step %s: %s sends %s with 2 items, so %s repeat %s with one item and with three, expecting what %s expects: "+
		"a backend whose logic changes with the number of items (a loop bound, a limit, a check on the first or last only) "+
		"passes on 2 and fails on one or three. The third item reads its own resource where the second does%s",
		st.ID, fx.ID, il.field.Name, strings.Join(ids, " and "), st.ID, st.ID, many)
}

const lotsOfItems = 12

func (p *Plan) manyItems(lib *Library, st *chain.Step, il itemList, third any, thirds map[string]string) string {
	list := st.Body[il.key].([]any)
	items := []any{cloneBody(list[0]), cloneBody(list[1]), cloneBody(third)}
	prev := third
	for n := len(items) + 1; n <= lotsOfItems; n++ {
		next := cloneBody(prev)
		if item, ok := next.(map[string]any); ok {
			distinctItem(item, il.field.Fields)
		}
		for _, sec := range p.seconds {
			if sec.reader != st.ID || !readsValue(list[1], sec.clone) {
				continue
			}
			id, steps := p.itemProducer(sec, n)
			p.Chain.Steps = append(p.Chain.Steps, steps...)
			next = rewriteRefs(next, map[string]string{thirds[sec.src]: id})
			thirds[sec.src] = id
		}
		items = append(items, next)
		prev = next
	}
	v := copyStep(st, p.freeStepID(fmt.Sprintf("%s_%d_%s", st.ID, lotsOfItems, il.field.Name)))
	v.Export = nil
	v.Body[il.key] = items
	p.freshen(lib, v)
	renameStepRefs(v, st.ID, v.ID)
	v.Description = fmt.Sprintf("as %s, but %s holds %s, so a cap on the number of items applied or reported shows.", st.ID, il.field.Name, itemCount(lotsOfItems))
	p.assertEcho(v)
	p.Chain.Steps = append(p.Chain.Steps, v)
	m, err := p.cat.Lookup(st.Call)
	if err != nil {
		return v.ID
	}
	listPath, verdict, _ := strings.Cut(chain.ItemEnvelope(), "[].")
	out := catalog.DescribeMessage(m.Output()).Fields
	if listPath == "" || verdict == "" || !chain.ItemEnvelopeDeclared(out) {
		return v.ID
	}
	v.Expect = append(v.Expect,
		chain.Expectation{Path: fmt.Sprintf("%s.%d.%s", listPath, lotsOfItems-1, verdict), Equals: chain.EnvelopeOK()},
		chain.Expectation{Path: fmt.Sprintf("%s.%d", listPath, lotsOfItems), Exists: boolPtr(false)})
	if p.middles == nil {
		p.middles = map[*chain.Step]string{}
	}
	p.middles[v] = listPath
	results, _ := catalog.FieldAt(out, chain.SplitPath(listPath))
	middle := map[int]bool{}
	for i := 1; i < lotsOfItems-1; i++ {
		middle[i] = true
	}
	p.Chain.Steps = append(p.Chain.Steps, p.reportedMatchesStored(lib, v, il.key, results, listPath, middle)...)
	return v.ID
}

func (p *Plan) trimMiddleItems() {
	for st, listPath := range p.middles {
		kept := st.Expect[:0]
		for _, ex := range st.Expect {
			rest, ok := strings.CutPrefix(ex.Path, listPath+".")
			head, _, nested := strings.Cut(rest, ".")
			if n, err := strconv.Atoi(head); ok && nested && err == nil && n > 0 && n < lotsOfItems-1 {
				continue
			}
			kept = append(kept, ex)
		}
		st.Expect = kept
	}
}

func itemCount(n int) string {
	return fmt.Sprintf("%d %s", n, pluralVerb(n, "item", "items"))
}

func readsValue(v any, id string) bool {
	for _, ref := range allStepRefs(v) {
		if ref[0] == id {
			return true
		}
	}
	return false
}

func (p *Plan) reserveStepID(base string) string {
	id := p.freeStepID(base)
	if p.reserved == nil {
		p.reserved = map[string]bool{}
	}
	p.reserved[id] = true
	return id
}

func (p *Plan) thirdProducer(sec producerSecond) (string, []*chain.Step) {
	return p.itemProducer(sec, 3)
}

func (p *Plan) itemProducer(sec producerSecond, n int) (string, []*chain.Step) {
	orig := p.stepByID(sec.src)
	id := p.reserveStepID(sec.src)
	clone := copyStep(orig, id)
	suffix := strings.TrimPrefix(id, sec.src+"_")
	if m, err := p.cat.Lookup(orig.Call); err == nil {
		distinctProducerAt(clone.Body, catalog.DescribeMessage(m.Input()).Fields, suffix, n-1)
	}
	renameStepRefs(clone, sec.src, id)
	clone.Description = fmt.Sprintf("a third %s with its own values, for the third item.", shortRPC(orig.Call))
	if n != 3 {
		clone.Description = fmt.Sprintf("another %s with its own values, for item %d.", shortRPC(orig.Call), n)
	}
	made := []*chain.Step{clone}
	for _, prep := range p.preps[sec.clone] {
		src := p.stepByID(prep)
		if src == nil {
			continue
		}
		cid := p.reserveStepID(prep)
		c := copyStep(src, cid)
		renameStepRefs(c, sec.src, id)
		renameStepRefs(c, prep, cid)
		p.distinctPreparation(c, cid, prep)
		made = append(made, c)
	}
	return id, made
}

func (p *Plan) assertEcho(st *chain.Step) {
	if !AssertsOnlyVerdict(st) {
		return
	}
	m, err := p.cat.Lookup(st.Call)
	if err != nil {
		return
	}
	in := catalog.DescribeMessage(m.Input()).Fields
	for _, fd := range catalog.DescribeMessage(m.Output()).Fields {
		if fd.Kind != "message" || fd.Repeated || fd.MapKey != "" || fd.Name == chain.EnvelopeField() || IsVerdictFieldName(fd.Name) {
			continue
		}
		for _, sf := range fd.Fields {
			key, ok := namecase.LookupKey(st.Body, sf.Name)
			if !ok {
				continue
			}
			path := fd.Name + "." + sf.Name
			switch v := st.Body[key].(type) {
			case string:
				if IsEntityIDField(sf.Name) && !sf.Repeated && wholeReference(v) {
					st.Expect = append(st.Expect, chain.Expectation{Path: path, Equals: v})
				}
			case []any:
				for _, f := range in {
					if f.Name == sf.Name && f.Repeated && sf.Repeated && sf.Kind == "message" && len(v) > 0 {
						st.Expect = append(st.Expect,
							chain.Expectation{Path: fmt.Sprintf("%s.%d", path, len(v)-1), Exists: boolPtr(true)},
							chain.Expectation{Path: fmt.Sprintf("%s.%d", path, len(v)), Exists: boolPtr(false)})
					}
				}
			}
		}
	}
}
