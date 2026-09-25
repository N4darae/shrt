package contract

import (
	"sort"
	"strconv"
	"strings"

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
		if !ok || !increaseWord.MatchString(c.Summary) {
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
