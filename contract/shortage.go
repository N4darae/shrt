package contract

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

func itemAt(body map[string]any, path string) map[string]any {
	segs := chain.SplitPath(path)
	var cur any = body
	for _, seg := range segs[:len(segs)-1] {
		switch t := cur.(type) {
		case map[string]any:
			cur = t[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(t) {
				return nil
			}
			cur = t[i]
		default:
			return nil
		}
	}
	m, _ := cur.(map[string]any)
	return m
}

func (p *Plan) entityRefsBeside(body map[string]any, path string) []string {
	item := itemAt(body, path)
	out := []string{}
	for _, v := range item {
		text, ok := v.(string)
		if !ok {
			continue
		}
		src, isRef := refSource(text)
		if !isRef || src == "vars" || src == "env" {
			continue
		}
		if prod := p.stepByID(src); prod != nil && !chain.IsReadOnlyCall(prod.Call) {
			out = append(out, text)
		}
	}
	sort.Strings(out)
	return out
}

func (p *Plan) suppliedFor(lib *Library, refs []string) (int64, bool) {
	if len(refs) == 0 {
		return 0, false
	}
	want := map[string]bool{}
	for _, r := range refs {
		want[r] = true
	}
	total, found := int64(0), false
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			names := false
			for _, item := range t {
				if s, ok := item.(string); ok && want[s] {
					names = true
				}
			}
			if names {
				for k, item := range t {
					if !isQuantityName(k) {
						continue
					}
					if n, ok := numericValue(item); ok && n > 0 {
						total += n
						found = true
					}
				}
			}
			for _, item := range t {
				walk(item)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	for _, st := range p.Chain.Steps {
		if st.AllowFail || chain.IsReadOnlyCall(st.Call) || isRefusalStep(st) {
			continue
		}
		c, ok := lib.Get(canonicalCall(p.cat, st.Call))
		if !ok || !increaseWord.MatchString(c.Summary) && !c.Effects.increases() {
			continue
		}
		walk(map[string]any(st.Body))
	}
	return total, found
}

func (p *Plan) noteShortageQuantity(st *chain.Step, unknown []string) {
	if len(unknown) == 0 {
		p.note("step %s: each shortage asks for one more than the stock the chain's own writes added to that item, "+
			"a quantity as small as the fixtures' own, so a cap on the quantity field (at most 99, say) does not refuse it for another reason and hide whether the shortage is refused", st.ID)
		return
	}
	p.note("step %s: no write in the chain adds stock to the item %s short, so %s %s %s: a backend that caps the "+
		"quantity field refuses that for another reason; plan the rpc that adds it (needs:) to have one more than "+
		"the added stock asked for instead", st.ID, strings.Join(unknown, ", "), pluralVerb(len(unknown), "it", "they"),
		pluralVerb(len(unknown), "asks for", "ask for"), overdrawValue)
}

func (p *Plan) shortageQuantity(lib *Library, body map[string]any, path string) (string, bool) {
	supplied, ok := p.suppliedFor(lib, p.entityRefsBeside(body, path))
	if !ok {
		return overdrawValue, false
	}
	return strconv.FormatInt(supplied+1, 10), true
}

func (p *Plan) probeExactStock(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || effectOutcome(st) != outcomeSuccess {
			continue
		}
		if m, f, ok := p.shortageFailure(lib, st); ok {
			p.addExactStockProbe(lib, st, m, f)
		}
	}
}

func (p *Plan) addExactStockProbe(lib *Library, st *chain.Step, m *catalog.Method, f Failure) {
	source, paths := p.shortagePaths(st, m)
	if len(paths) == 0 {
		return
	}
	base := st
	if source != nil {
		base = source
	}
	id := p.freeStepID(st.ID + "_exact_stock")
	exact := copyStep(base, id)
	if source != nil {
		exact = copyStep(source, p.freeStepID(source.ID+"_for_"+id))
	}
	exact.Export = nil
	seen := map[string]bool{}
	set := []string{}
	for _, path := range paths {
		refs := p.entityRefsBeside(base.Body, path)
		supplied, ok := p.suppliedFor(lib, refs)
		key := strings.Join(refs, ",")
		if !ok || seen[key] {
			p.gap("step %s: no write in the chain adds a known quantity to the item at %s on its own, so no probe asks "+
				"for exactly the stock on hand (%s): plan the rpc that adds it (needs:)", st.ID, path, f.Label())
			return
		}
		seen[key] = true
		setBodyPath(exact.Body, path, strconv.FormatInt(supplied, 10))
		set = append(set, fmt.Sprintf("%s = %d", path, supplied))
	}
	p.freshen(lib, exact)
	steps := []*chain.Step{exact}
	if source != nil {
		renameStepRefs(exact, source.ID, exact.ID)
		exact.Description = fmt.Sprintf("as %s, but asking for exactly the stock this chain added (%s), for %s.", source.ID, strings.Join(set, ", "), id)
		p.assertEcho(exact)
		act := probeStep(st, id)
		renameStepRefs(act, source.ID, exact.ID)
		renameStepRefs(act, st.ID, act.ID)
		act.Description = fmt.Sprintf("%s when every line asks for exactly the stock on hand: it succeeds and leaves none, since %s is refused only for more than there is.", st.ID, f.Label())
		steps = append(steps, act)
	} else {
		renameStepRefs(exact, st.ID, exact.ID)
		exact.Description = fmt.Sprintf("as %s, asking for exactly the stock this chain added (%s): it succeeds and leaves none, since %s is refused only for more than there is.", st.ID, strings.Join(set, ", "), f.Label())
	}
	p.Chain.Steps = append(p.Chain.Steps, steps...)
	p.note("step %s: %s asks for exactly the stock the chain added (%s) and expects success, the read after it a level of 0: "+
		"the shortage probes ask for one more, so together they pin the boundary, and a backend that refuses a quantity "+
		"equal to the stock on hand (>= where > is meant) fails here", st.ID, id, strings.Join(set, ", "))
}
