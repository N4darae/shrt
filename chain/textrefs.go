package chain

import (
	"fmt"
	"strings"
)

func varStructures(s *Step, vars map[string]any) []string {
	out := []string{}
	check := func(where, text string, header bool) {
		refs := collectRefs(text)
		if !header && len(refs) == 1 && strings.TrimSpace(text) == "${"+refs[0]+"}" {
			return
		}
		for _, ref := range refs {
			r := ParseRef(ref)
			if r.Kind != RefVars || r.Err != nil || r.Rest == "" {
				continue
			}
			v, ok := Get(vars, r.Rest)
			if !ok {
				continue
			}
			kind := ""
			switch v.(type) {
			case map[string]any:
				kind = "map"
			case []any:
				kind = "list"
			default:
				continue
			}
			how := "is interpolated inside other text in " + where
			if header {
				how = "fills " + where
			}
			out = append(out, fmt.Sprintf("${%s} %s (%q), but that var holds a %s, which has no text form; use one scalar "+
				"field of it (${vars.%s.<field>})", ref, how, text, kind, r.Rest))
		}
	}
	walkLeaves(s.Body, "", "", func(path, _, t string) { check(path, t, false) })
	for _, name := range SortedKeys(s.Headers) {
		check("header "+name, s.Headers[name], true)
	}
	return out
}

func (c *Chain) VarStructureProblems(vars map[string]any) []string {
	out := []string{}
	for i, s := range c.Steps {
		if s == nil {
			continue
		}
		for _, why := range varStructures(s, vars) {
			out = append(out, fmt.Sprintf("step %q (step %d): %s", s.ID, i+1, why))
		}
	}
	return out
}
