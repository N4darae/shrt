package contract

import (
	"fmt"
	"iter"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

type Situation struct {
	Carrier string `json:"carrier,omitempty"`
	State   string `json:"state,omitempty"`
	List    string `json:"list,omitempty"`
	Items   int    `json:"items,omitempty"`
}

func (s Situation) String() string {
	parts := []string{}
	if s.State != "" {
		parts = append(parts, fmt.Sprintf("on %s in %s", withArticle(s.Carrier), s.State))
	}
	if s.List != "" {
		parts = append(parts, fmt.Sprintf("%s of %s", s.List, itemCount(s.Items)))
	}
	return strings.Join(parts, ", ")
}

func Situations(c *chain.Chain, lib *Library, cat *catalog.Catalog) map[string]Situation {
	out := map[string]Situation{}
	if c == nil || lib == nil {
		return out
	}
	p := newPlan(&Plan{cat: cat, lib: lib, Chain: c})
	states := map[string]string{}
	for _, st := range c.Steps {
		if st == nil {
			continue
		}
		ctr, m, ok := p.contractOf(lib, canonicalCall(cat, st.Call))
		if !ok {
			continue
		}
		succeeds := effectOutcome(st) == outcomeSuccess
		write := !chain.IsReadOnlyCall(st.Call) && !m.Streaming()
		sit, known := Situation{}, false
		for _, e := range p.entityStates(st, ctr) {
			values, short := e.state.EnumValues[1:], enumShort(e.state.EnumValues)
			cur, seen := states[e.producer.ID]
			if !seen {
				cur = stateAsserted(e.producer, e)
				if pc, ok := lib.Get(canonicalCall(cat, e.producer.Call)); ok && cur == "" {
					cur = stateIn([]string{pc.Exports[e.carrier], pc.Summary}, values, short)
				}
			}
			if !known && cur != "" {
				sit, known = Situation{Carrier: e.carrier, State: short[cur]}, true
				sit.List, sit.Items = p.itemsAround(st, e.producer)
			}
			if succeeds {
				if next := stateAsserted(st, e); next != "" {
					cur = next
				} else if write && carrierField(m, e.itemMsg) != "" {
					if next := stateIn([]string{ctr.Exports[e.carrier], ctr.Summary}, values, short); next != "" {
						cur = next
					}
				}
			}
			states[e.producer.ID] = cur
		}
		if known && write && succeeds {
			out[st.ID] = sit
		}
	}
	return out
}

func stateAsserted(st *chain.Step, e stateEntity) string {
	for _, x := range st.Expect {
		if v, ok := x.Equals.(string); ok && x.Path == e.carrier+"."+e.state.Name && slices.Contains(e.state.EnumValues, v) {
			return v
		}
	}
	return ""
}

func (p *Plan) itemsAround(st, producer *chain.Step) (string, int) {
	for _, s := range []*chain.Step{st, producer} {
		for _, il := range p.sentLists(s) {
			if list, _ := s.Body[il.key].([]any); len(list) > 0 {
				return il.field.Name, len(list)
			}
		}
	}
	return "", 0
}

type StateGap struct {
	RPC     string   `json:"rpc"`
	Carrier string   `json:"carrier"`
	State   string   `json:"state"`
	List    string   `json:"list,omitempty"`
	Missing []int    `json:"missing_items,omitempty"`
	Sent    []int    `json:"sent_items,omitempty"`
	Chains  []string `json:"chains,omitempty"`
	Why     string   `json:"why,omitempty"`
}

func StateGaps(chains []*chain.Chain, plans map[string]*Plan, lib *Library, cat *catalog.Catalog) []StateGap {
	type key struct{ rpc, state string }
	sent := map[key]map[int]bool{}
	by := map[key][]string{}
	for _, c := range chains {
		if c == nil {
			continue
		}
		sits := Situations(c, lib, cat)
		for _, st := range c.Steps {
			sit, ok := sits[st.ID]
			if !ok {
				continue
			}
			k := key{canonicalCall(cat, st.Call), sit.State}
			if sent[k] == nil {
				sent[k] = map[int]bool{}
			}
			sent[k][sit.Items] = true
			if !slices.Contains(by[k], c.Name) {
				by[k] = append(by[k], c.Name)
			}
		}
	}
	out := []StateGap{}
	for _, rpc := range chain.SortedKeys(plans) {
		p := plans[rpc]
		if p == nil || chain.IsReadOnlyCall(rpc) {
			continue
		}
		planned := map[key]*StateGap{}
		order := []key{}
		sits := Situations(p.Chain, lib, cat)
		for _, st := range p.Chain.Steps {
			sit, ok := sits[st.ID]
			if !ok || canonicalCall(cat, st.Call) != rpc {
				continue
			}
			k := key{rpc, sit.State}
			g := planned[k]
			if g == nil {
				g = &StateGap{RPC: rpc, Carrier: sit.Carrier, State: sit.State, List: sit.List, Chains: by[k]}
				planned[k], order = g, append(order, k)
			}
			if g.Why == "" {
				g.Why = p.skippedWhy(st, sit.State)
			}
			if !sent[k][sit.Items] && !slices.Contains(g.Missing, sit.Items) {
				g.Missing = append(g.Missing, sit.Items)
			}
		}
		for _, k := range order {
			g := planned[k]
			if len(g.Missing) == 0 {
				continue
			}
			for n := range sent[k] {
				g.Sent = append(g.Sent, n)
			}
			sort.Ints(g.Missing)
			sort.Ints(g.Sent)
			out = append(out, *g)
		}
	}
	return out
}

func (p *Plan) skippedWhy(st *chain.Step, state string) string {
	c, _, ok := p.contractOf(p.lib, st.Call)
	if !ok {
		return ""
	}
	for _, e := range p.entityStates(st, c) {
		if sk, ok := p.skippedStart(p.lib, st, c, e); ok && sk.state == state {
			return sk.why()
		}
	}
	return ""
}

func (g StateGap) Line() string {
	what := "no chain calls it so"
	switch {
	case len(g.Sent) > 0 && g.List != "":
		what = fmt.Sprintf("no chain sends %s (%s %s %s)", itemCounts(g.Missing, g.List), strings.Join(clipList(g.Chains, 3), ", "), pluralVerb(len(g.Chains), "sends", "send"), itemCounts(g.Sent, ""))
	case g.List != "":
		what += "; its plan sends " + itemCounts(g.Missing, g.List)
	}
	return fmt.Sprintf("%s on %s: %s", shortRPC(g.RPC), withArticle(g.State+" "+g.Carrier), what)
}

func itemCounts(ns []int, list string) string {
	words := make([]string, len(ns))
	for i, n := range ns {
		words[i] = strconv.Itoa(n)
	}
	text := words[len(words)-1]
	if len(words) > 1 {
		text = strings.Join(words[:len(words)-1], ", ") + " or " + text
	}
	if list == "" {
		return text
	}
	return text + " " + pluralVerb(ns[len(ns)-1], strings.TrimSuffix(list, "s"), list)
}

func chainSteps(chains []*chain.Chain) iter.Seq2[*chain.Chain, *chain.Step] {
	return func(yield func(*chain.Chain, *chain.Step) bool) {
		for _, c := range chains {
			if c == nil {
				continue
			}
			for _, s := range c.Steps {
				if s != nil && !yield(c, s) {
					return
				}
			}
		}
	}
}
