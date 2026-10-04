package chain

import (
	"slices"
	"strings"

	"github.com/N4darae/shrt/namecase"
)

func collectRefs(v any) []string {
	out := []string{}
	walkText(v, "", func(_, s string) {
		for _, m := range refPattern.FindAllStringSubmatch(s, -1) {
			out = append(out, strings.TrimSpace(m[1]))
		}
	})
	return out
}

func hasRef(v any) bool {
	s, ok := v.(string)
	return ok && refPattern.MatchString(s)
}

func (c *Chain) UnusedVarNames(supplied map[string]any) []string {
	if len(supplied) == 0 {
		return nil
	}
	declared := c.DeclaredVarNames()
	return slices.DeleteFunc(SortedKeys(supplied), func(name string) bool { return slices.Contains(declared, name) })
}

func (c *Chain) DeclaredVarNames() []string {
	seen := map[string]bool{}
	for name := range c.Vars {
		seen[name] = true
	}
	note := func(v any) {
		for _, ref := range collectRefs(v) {
			if name, ok := strings.CutPrefix(ref, "vars."); ok {
				seen[name] = true
			}
		}
	}
	note(c.Vars)
	for _, s := range c.Steps {
		if s == nil {
			continue
		}
		note(s.Body)
		note(s.Headers)
		note(s.Export)
		for _, e := range s.Expect {
			for _, v := range e.Operands() {
				note(v)
			}
		}
	}
	return SortedKeys(seen)
}

func IsGeneratorRef(s string) bool {
	refs := collectRefs(s)
	if len(refs) != 1 || strings.TrimSpace(s) != "${"+refs[0]+"}" {
		return false
	}
	kind := ParseRef(refs[0]).Kind
	return kind == RefUUID || kind == RefClock
}

func IsStableRef(s string) bool {
	refs := collectRefs(s)
	return len(refs) == 1 && strings.TrimSpace(s) == "${"+refs[0]+"}" && !IsGeneratorRef(s)
}

func HasReference(s string) bool { return refPattern.MatchString(s) }

func CanonicalRefs(text string) string {
	return refPattern.ReplaceAllStringFunc(text, func(m string) string {
		return "${" + CanonicalRef(refPattern.FindStringSubmatch(m)[1]) + "}"
	})
}

func FoldedRefs(text string) string {
	return refPattern.ReplaceAllStringFunc(text, func(m string) string {
		ref := CanonicalRef(refPattern.FindStringSubmatch(m)[1])
		if !strings.HasPrefix(ref, "steps.") {
			return "${" + ref + "}"
		}
		parts := strings.SplitN(ref, ".", 4)
		if len(parts) == 4 {
			parts[3] = namecase.Fold(parts[3])
		}
		return "${" + strings.Join(parts, ".") + "}"
	})
}

func CanonicalRef(expr string) string {
	r := ParseRef(expr)
	if r.Kind != RefStep || r.Err != nil || r.Head == "" {
		return r.Expr
	}
	section, tail := "response", r.Rest
	if first, sub, _ := strings.Cut(r.Rest, "."); first == "request" || first == "response" {
		section, tail = first, sub
	}
	if tail == "" {
		return "steps." + r.Head + "." + section
	}
	return "steps." + r.Head + "." + section + "." + tail
}

func (s *Step) SendReferences() []string {
	if s == nil {
		return nil
	}
	values := []any{s.Body}
	for _, v := range s.Headers {
		values = append(values, v)
	}
	return collectRefs(values)
}

func (e Expectation) References() []string {
	return collectRefs(e.Operands())
}

func (s *Step) References() []string {
	if s == nil {
		return nil
	}
	refs := s.SendReferences()
	for _, e := range s.Expect {
		refs = append(refs, e.References()...)
	}
	return refs
}

func (c *Chain) MissingVars(supplied map[string]any) []string {
	undeclared, _ := ExternalInputs(c)
	return slices.DeleteFunc(undeclared, func(name string) bool { _, ok := supplied[name]; return ok })
}
