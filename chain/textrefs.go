package chain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
)

func headerStructures(s *Step, responses map[string]*catalog.Method, exports map[string]exportOrigin) []string {
	out := []string{}
	for _, name := range sortedHeaderNames(s.Headers) {
		value := s.Headers[name]
		for _, ref := range collectRefs(value) {
			src, where, collection, ok := refSourceField(ParseRef(ref), responses, exports)
			if !ok || dynamicWellKnown[src.Message] {
				continue
			}
			kind := ""
			switch {
			case collection:
				kind = collectionKind(src)
			case isMessage(src) && !scalarWellKnown[src.Message]:
				kind = src.Message
				if kind == "" {
					kind = "message"
				}
			default:
				continue
			}
			out = append(out, fmt.Sprintf("${%s} fills header %s (%q), from %s, declared %s — a header carries text only, "+
				"and a message, list or map has no text form, so it would be sent as Go syntax (map[...] or [...]) instead of "+
				"anything the backend reads, and shrt run refuses the chain before sending anything. Reference one scalar "+
				"field of it instead", ref, name, value, where, kind))
		}
	}
	return out
}

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
			out = append(out, fmt.Sprintf("${%s} %s (%q), but that var holds a %s — a %s has no text form, so it would be "+
				"sent as Go syntax (map[...] or [...]) instead of anything the backend reads, and shrt run refuses the chain "+
				"before sending anything. Interpolate one scalar field of it instead (${vars.%s.<field>})", ref, how, text, kind, kind, r.Rest))
		}
	}
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch t := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				p := k
				if path != "" {
					p = path + "." + k
				}
				walk(t[k], p)
			}
		case []any:
			for i, x := range t {
				walk(x, fmt.Sprintf("%s.%d", path, i))
			}
		case string:
			check(path, t, false)
		}
	}
	walk(s.Body, "")
	for _, name := range sortedHeaderNames(s.Headers) {
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
