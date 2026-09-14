package contract

import (
	"sort"

	"github.com/N4darae/shrt/chain"
)

func PrereqsFor(lib *Library) func(string) []chain.Prereq {
	return func(rpc string) []chain.Prereq {
		seen := map[string]bool{}
		out := []chain.Prereq{}
		add := func(node, edge string) {
			target, _ := SplitNode(node)
			if target == "" || target == rpc || seen[target] {
				return
			}
			seen[target] = true
			out = append(out, chain.Prereq{RPC: target, Edge: edge})
		}
		if c, ok := lib.Get(rpc); ok {
			for _, n := range c.Needs {
				add(n, "needs")
			}
			for _, f := range c.Fields {
				addFieldPrereq(f, add)
			}
			for _, a := range c.Aliases {
				for _, f := range a.Fields {
					addFieldPrereq(f, add)
				}
			}
		}
		for _, n := range lib.RequiredBy(rpc) {
			add(n, "before")
		}
		sort.Slice(out, func(i, j int) bool { return out[i].RPC < out[j].RPC })
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
