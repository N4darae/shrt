package contract

import "github.com/N4darae/shrt/catalog"

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
		for _, rpc := range sortedKeys(o.RPCs) {
			for _, target := range referencedNodes(o.RPCs[rpc]) {
				m, err := cat.Lookup(target)
				if err != nil {
					continue
				}
				if _, declared := lib.Get(m.FullName); !declared {
					out.Sites++
					missing[m.FullName] = DomainOf(m)
				}
			}
		}
	}
	if len(missing) > 0 {
		domains := map[string]bool{}
		for _, domain := range missing {
			domains[domain] = true
		}
		out.RPCs, out.Domains = sortedKeys(missing), sortedKeys(domains)
	}
	return out
}

func referencedNodes(c *RPCContract) []string {
	nodes := []string{}
	collect := func(fields map[string]*FieldContract) {
		for _, name := range sortedKeys(fields) {
			f := fields[name]
			for _, raw := range []string{f.From, f.SameAs} {
				if ref, err := ParseRef(raw); err == nil {
					nodes = append(nodes, ref.RPC)
				}
			}
		}
	}
	collect(c.Fields)
	for _, alias := range sortedKeys(c.Aliases) {
		collect(c.Aliases[alias].Fields)
	}
	for _, node := range append(append([]string{}, c.Needs...), c.Before...) {
		rpc, _ := SplitNode(node)
		nodes = append(nodes, rpc)
	}
	return nodes
}
