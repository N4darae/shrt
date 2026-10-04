package contract

import (
	"strings"

	"github.com/N4darae/shrt/chain"
)

func bodyText(v any, out *strings.Builder) {
	mapStrings(v, func(t string) string {
		out.WriteString(t + "\n")
		return t
	})
}

func (p *Plan) statesBefore(lib *Library, t *listTarget, values []string, short map[string]string) map[string]string {
	out := map[string]string{}
	refs := map[string]string{}
	for _, prod := range t.producers {
		refs[prod.ID] = "${" + prod.ID + "." + t.carrier + "." + t.itemID + "}"
	}
	for _, s := range p.Chain.Steps {
		if s == t.step {
			break
		}
		if s.AllowFail || chain.IsReadOnlyCall(s.Call) || isRefusalStep(s) || s.SkipAuth {
			continue
		}
		m, err := p.cat.Lookup(s.Call)
		if err != nil {
			continue
		}
		carrier := carrierField(m, t.itemMsg)
		if carrier == "" {
			continue
		}
		c, ok := lib.Get(m.FullName)
		if !ok {
			continue
		}
		var text strings.Builder
		bodyText(map[string]any(s.Body), &text)
		for _, prod := range t.producers {
			if prod == s || !strings.Contains(text.String(), refs[prod.ID]) {
				continue
			}
			if v := stateIn([]string{c.Exports[carrier], c.Summary}, values, short); v != "" {
				out[prod.ID] = v
			}
		}
	}
	return out
}

func movedNote(producers []*chain.Step, moved map[string]string, short map[string]string) string {
	parts := []string{}
	for _, prod := range producers {
		if v, ok := moved[prod.ID]; ok {
			parts = append(parts, prod.ID+" is "+short[v])
		}
	}
	return "Earlier steps of the chain already moved some fixtures, so each filtered list expects them where those steps " +
		"left them (" + strings.Join(parts, ", ") + ") and the other fixtures fill the states still missing"
}

func assignStates(producers []*chain.Step, moved map[string]string, initial string, transitions []transition) (map[*chain.Step]string, map[*chain.Step]*transition) {
	states := map[*chain.Step]string{}
	moves := map[*chain.Step]*transition{}
	covered := map[string]bool{}
	for _, prod := range producers {
		if v, ok := moved[prod.ID]; ok {
			states[prod] = v
			covered[v] = true
		}
	}
	wanted := []*transition{}
	if initial != "" && !covered[initial] {
		wanted = append(wanted, nil)
	}
	for i := range transitions {
		if !covered[transitions[i].value] {
			wanted = append(wanted, &transitions[i])
		}
	}
	k := 0
	for _, prod := range producers {
		if _, ok := states[prod]; ok {
			continue
		}
		if k < len(wanted) {
			if tr := wanted[k]; tr != nil {
				moves[prod] = tr
				states[prod] = tr.value
			} else {
				states[prod] = initial
			}
			k++
			continue
		}
		if initial != "" {
			states[prod] = initial
		}
	}
	return states, moves
}
