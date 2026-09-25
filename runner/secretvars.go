package runner

import (
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
)

func varsUsedInTheOpen(c *chain.Chain, redactor *pathmask.Masker) map[string]bool {
	open := map[string]bool{}
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch t := v.(type) {
		case map[string]any:
			for k, item := range t {
				walk(item, pathmask.Join(path, k))
			}
		case []any:
			for i, item := range t {
				walk(item, pathmask.Join(path, pathmask.IndexKey(i)))
			}
		case string:
			if path != "" && redactor.Masks(path) {
				return
			}
			for _, ref := range chain.VarRefs(t) {
				open[ref] = true
			}
		}
	}
	for _, s := range c.Steps {
		if s == nil {
			continue
		}
		walk(orEmpty(s.Body), "")
		for name, template := range s.Headers {
			if !secretHeader(name) {
				walk(template, "")
			}
		}
		for _, e := range s.Expect {
			if redactor.Masks(e.Path) {
				continue
			}
			walk([]any{e.Equals, e.NotEqual, e.Contains}, "")
		}
	}
	return open
}

func secretVar(ref, template string, open map[string]bool, redactor *pathmask.Masker) bool {
	if strings.TrimSpace(template) == ref || !open[ref] {
		return true
	}
	name := strings.TrimSuffix(strings.TrimPrefix(ref, "${vars."), "}")
	if head, _, ok := strings.Cut(name, "."); ok {
		name = head
	}
	return secretHeader(name) || redactor.Masks(name)
}
