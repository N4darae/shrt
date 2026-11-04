package contract

import (
	"fmt"

	"github.com/N4darae/shrt/catalog"
)

// Planning uses full RPC names for both graph nodes and field wiring. Keep this
// view separate so lint and editors still see the names the caller wrote.
func (l *Library) forPlanning(cat *catalog.Catalog) (*Library, error) {
	if l == nil {
		return nil, nil
	}
	overlays := make([]*Overlay, 0, len(l.Overlays))
	defined := map[string]definedAt{}
	for _, o := range l.Overlays {
		copy := *o
		copy.RPCs = make(map[string]*RPCContract, len(o.RPCs))
		for _, rpc := range sortedRPCNames(o.RPCs) {
			full := canonicalCall(cat, rpc)
			if first, ok := defined[full]; ok && first.key != rpc {
				return nil, fmt.Errorf("contract keys %q (%s) and %q (%s) both resolve to %s: keep one contract for the rpc",
					first.key, first.path, rpc, o.SourcePath, full)
			}
			defined[full] = definedAt{key: rpc, path: o.SourcePath}
			c := *o.RPCs[rpc]
			c.Needs = planningNodes(c.Needs, cat)
			c.Before = planningNodes(c.Before, cat)
			c.Fields = planningFields(c.Fields, cat)
			if c.Aliases != nil {
				c.Aliases = make(map[string]*AliasContract, len(o.RPCs[rpc].Aliases))
				for name, alias := range o.RPCs[rpc].Aliases {
					a := *alias
					a.Fields = planningFields(a.Fields, cat)
					c.Aliases[name] = &a
				}
			}
			copy.RPCs[full] = &c
		}
		overlays = append(overlays, &copy)
	}
	return NewLibrary(overlays), nil
}

func planningNodes(nodes []string, cat *catalog.Catalog) []string {
	if nodes == nil {
		return nil
	}
	out := make([]string, len(nodes))
	for i, node := range nodes {
		rpc, alias := SplitNode(node)
		out[i] = canonicalCall(cat, rpc)
		if alias != "" {
			out[i] += "@" + alias
		}
	}
	return out
}

func planningFields(fields map[string]*FieldContract, cat *catalog.Catalog) map[string]*FieldContract {
	if fields == nil {
		return nil
	}
	out := make(map[string]*FieldContract, len(fields))
	for name, field := range fields {
		f := *field
		f.From = planningRef(f.From, cat)
		f.SameAs = planningRef(f.SameAs, cat)
		out[name] = &f
	}
	return out
}

func planningRef(raw string, cat *catalog.Catalog) string {
	ref, err := ParseRef(raw)
	if err != nil {
		return raw
	}
	full := canonicalCall(cat, ref.RPC)
	if full == ref.RPC {
		return raw
	}
	ref.RPC = full
	separator := RefSeparator
	if UsesLegacySeparator(raw) {
		separator = LegacyRefSeparator
	}
	return ref.Node() + separator + ref.Path
}
