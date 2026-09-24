package contract

import (
	"sort"

	"github.com/N4darae/shrt/chain"
)

func PrereqsFor(lib *Library) func(string) []chain.Prereq {
	return func(rpc string) []chain.Prereq {
		seen := map[string]bool{}
		out := []chain.Prereq{}
		addFor := func(node, edge, forAlias string) {
			target, alias := SplitNode(node)
			p := chain.Prereq{RPC: target, Alias: alias, Edge: edge, For: forAlias}
			key := p.Node() + "\x00" + forAlias
			if target == "" || target == rpc || seen[key] {
				return
			}
			seen[key] = true
			out = append(out, p)
		}
		add := func(node, edge string) { addFor(node, edge, "") }
		if c, ok := lib.Get(rpc); ok {
			for _, n := range c.Needs {
				add(n, "needs")
			}
			for _, f := range c.Fields {
				addFieldPrereq(f, add)
			}
			for name, a := range c.Aliases {
				for _, f := range a.Fields {
					addFieldPrereq(f, func(node, edge string) { addFor(node, edge, name) })
				}
			}
		}
		for _, n := range lib.RequiredBy(rpc) {
			add(n, "before")
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Node() != out[j].Node() {
				return out[i].Node() < out[j].Node()
			}
			return out[i].For < out[j].For
		})
		return out
	}
}

func addFieldPrereq(f *FieldContract, add func(node, edge string)) {
	if f == nil {
		return
	}
	if ref, err := ParseRef(f.From); err == nil {
		add(ref.Node(), "from")
	}
	if ref, err := ParseRef(f.SameAs); err == nil {
		add(ref.Node(), "same_as")
	}
}
