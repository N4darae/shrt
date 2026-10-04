package runner

import (
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
)

func varsUsedInTheOpen(c *chain.Chain, redactor *pathmask.Masker) map[string]bool {
	open := map[string]bool{}
	inTheOpen := func(v any, path string) {
		t, ok := v.(string)
		if !ok || path != "" && redactor.Masks(path) {
			return
		}
		for _, ref := range chain.VarRefs(t) {
			open[ref] = true
		}
	}
	for _, s := range c.Steps {
		if s == nil {
			continue
		}
		eachLeaf(orEmpty(s.Body), "", inTheOpen)
		for name, template := range s.Headers {
			if !secretHeader(name) {
				eachLeaf(template, "", inTheOpen)
			}
		}
		for _, e := range s.Expect {
			if redactor.Masks(e.Path) {
				continue
			}
			eachLeaf([]any{e.Equals, e.NotEqual, e.Contains}, "", inTheOpen)
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
