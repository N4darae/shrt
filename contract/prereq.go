package contract

import (
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
)

func PrereqsFor(lib *Library) func(string) []chain.Prereq {
	return func(rpc string) []chain.Prereq {
		seen := map[string]bool{}
		out := []chain.Prereq{}
		addField := func(node, edge, forAlias, field string) {
			target, alias := SplitNode(node)
			p := chain.Prereq{RPC: target, Alias: alias, Edge: edge, For: forAlias, Field: field}
			key := p.Node() + "\x00" + forAlias + "\x00" + field
			if target == "" || target == rpc || seen[key] {
				return
			}
			seen[key] = true
			out = append(out, p)
		}
		addFor := func(node, edge, forAlias string) { addField(node, edge, forAlias, "") }
		add := func(node, edge string) { addFor(node, edge, "") }
		if c, ok := lib.Get(rpc); ok {
			for _, n := range c.Needs {
				before := len(out)
				add(n, "needs")
				if len(out) > before {
					out[len(out)-1].Via = sameEffectRPCs(lib, out[len(out)-1].RPC)
				}
			}
			for _, field := range sortedFieldNames(c.Fields) {
				addFieldPrereq(c.Fields[field], func(node, edge string) { addField(node, edge, "", field) })
			}
			for name, a := range c.Aliases {
				for _, field := range sortedFieldNames(a.Fields) {
					addFieldPrereq(a.Fields[field], func(node, edge string) { addField(node, edge, name, field) })
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

func sameEffectRPCs(lib *Library, rpc string) []string {
	short := rpc
	if i := strings.LastIndex(rpc, "/"); i >= 0 {
		short = rpc[i+1:]
	}
	out := []string{}
	for _, other := range lib.RPCs() {
		c, ok := lib.Get(other)
		if !ok || other == rpc {
			continue
		}
		for name, f := range c.Fields {
			if f == nil || strings.Contains(name, ".") {
				continue
			}
			if m := perItemClause.FindStringSubmatch(f.Note); m != nil && strings.EqualFold(m[1], short) && !containsString(out, other) {
				out = append(out, other)
			}
		}
	}
	sort.Strings(out)
	return out
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
