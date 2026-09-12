package contract

import (
	"sort"

	"github.com/N4darae/shrt/catalog"
)

type OutOfReach struct {
	RPCs    []string `json:"rpcs"`
	Domains []string `json:"domains"`
	Sites   int      `json:"sites"`
}

func (o OutOfReach) Empty() bool { return len(o.RPCs) == 0 }

func ReferencedOutsideLibrary(lib *Library, cat *catalog.Catalog, onlyDomain string) OutOfReach {
	out := OutOfReach{}
	missing := map[string]string{}
	for _, o := range lib.Overlays {
		if onlyDomain != "" && o.Domain != onlyDomain {
			continue
		}
		for _, rpc := range sortedRPCNames(o.RPCs) {
			for _, target := range referencedNodes(o.RPCs[rpc]) {
				name, domain, ok := absentFromLibrary(target, lib, cat)
				if !ok {
					continue
				}
				out.Sites++
				missing[name] = domain
			}
		}
	}
	domains := map[string]bool{}
	for name, domain := range missing {
		out.RPCs = append(out.RPCs, name)
		domains[domain] = true
	}
	sort.Strings(out.RPCs)
	for d := range domains {
		out.Domains = append(out.Domains, d)
	}
	sort.Strings(out.Domains)
	return out
}

func referencedNodes(c *RPCContract) []string {
	nodes := []string{}
	collect := func(fields map[string]*FieldContract) {
		for _, name := range sortedFieldNames(fields) {
			f := fields[name]
			for _, raw := range []string{f.From, f.SameAs} {
				if raw == "" {
					continue
				}
				ref, err := ParseRef(raw)
				if err != nil {
					continue
				}
				nodes = append(nodes, ref.RPC)
			}
		}
	}
	collect(c.Fields)
	for _, alias := range sortedAliasNames(c.Aliases) {
		collect(c.Aliases[alias].Fields)
	}
	for _, node := range append(append([]string{}, c.Needs...), c.Before...) {
		rpc, _ := SplitNode(node)
		nodes = append(nodes, rpc)
	}
	return nodes
}

func absentFromLibrary(target string, lib *Library, cat *catalog.Catalog) (name, domain string, ok bool) {
	m, err := cat.Lookup(target)
	if err != nil {
		return "", "", false
	}
	if _, declared := lib.Get(m.FullName); declared {
		return "", "", false
	}
	return m.FullName, DomainOf(m), true
}
