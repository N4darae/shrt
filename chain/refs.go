package chain

import (
	"sort"
	"strings"
)

func collectRefs(v any) []string {
	out := []string{}
	walkStrings(v, func(s string) {
		for _, m := range refPattern.FindAllStringSubmatch(s, -1) {
			out = append(out, strings.TrimSpace(m[1]))
		}
	})
	return out
}

func walkStrings(v any, fn func(string)) {
	switch t := v.(type) {
	case string:
		fn(t)
	case map[string]any:
		for _, item := range t {
			walkStrings(item, fn)
		}
	case []any:
		for _, item := range t {
			walkStrings(item, fn)
		}
	}
}

func hasRef(v any) bool {
	s, ok := v.(string)
	return ok && refPattern.MatchString(s)
}

func (c *Chain) UnusedVarNames(supplied map[string]any) []string {
	if len(supplied) == 0 {
		return nil
	}
	referenced := map[string]bool{}
	for name := range c.Vars {
		referenced[name] = true
	}
	note := func(v any) {
		walkStrings(v, func(s string) {
			for _, ref := range collectRefs(s) {
				if name, ok := strings.CutPrefix(ref, "vars."); ok {
					referenced[name] = true
				}
			}
		})
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
			note(e.Equals)
			note(e.NotEqual)
			note(e.Contains)
		}
	}
	out := []string{}
	for name := range supplied {
		if !referenced[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (c *Chain) DeclaredVarNames() []string {
	seen := map[string]bool{}
	for name := range c.Vars {
		seen[name] = true
	}
	note := func(v any) {
		walkStrings(v, func(s string) {
			for _, ref := range collectRefs(s) {
				if name, ok := strings.CutPrefix(ref, "vars."); ok {
					seen[name] = true
				}
			}
		})
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
			note(e.Equals)
			note(e.NotEqual)
			note(e.Contains)
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func IsGeneratorRef(s string) bool {
	refs := collectRefs(s)
	if len(refs) != 1 || strings.TrimSpace(s) != "${"+refs[0]+"}" {
		return false
	}
	switch ParseRef(refs[0]).Kind {
	case RefUUID, RefClock:
		return true
	}
	return false
}

func IsStableRef(s string) bool {
	refs := collectRefs(s)
	if len(refs) != 1 || strings.TrimSpace(s) != "${"+refs[0]+"}" {
		return false
	}
	return !IsGeneratorRef(s)
}

func HasReference(s string) bool { return refPattern.MatchString(s) }

func (s *Step) References() []string {
	if s == nil {
		return nil
	}
	values := []any{s.Body}
	for _, v := range s.Headers {
		values = append(values, v)
	}
	for _, e := range s.Expect {
		values = append(values, e.Equals, e.NotEqual, e.Contains)
	}
	return collectRefs(values)
}
