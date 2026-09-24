package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestAMessageOrListInterpolatedInsideTextIsRefusedUpFront(t *testing.T) {
	cat := catalogtest.Shop()
	for _, ref := range []string{"${cp.product}", "${cp.status.details}", "${prod}"} {
		c := createProductNamed("x-" + ref + " y")
		if !lintErrorNaming(c, ref) {
			t.Errorf("name: \"x-%s y\" interpolates a message or list into text and lint did not error: %+v", ref, chain.Lint(c, cat))
		}
		if p := c.ResponseRefProblems(cat); len(p) == 0 || !strings.Contains(strings.Join(p, " "), ref) {
			t.Errorf("name: \"x-%s y\" must be refused before anything is sent, got %v", ref, p)
		}
	}
	for _, ref := range []string{"${pid}", "${cp.product.sku}", "${cp.status.code}"} {
		c := createProductNamed("x-" + ref + " y")
		if p := c.ResponseRefProblems(cat); len(p) != 0 {
			t.Errorf("a scalar inside text is fine: %q got %v", ref, p)
		}
	}
}

func TestResolvingAMessageInsideTextFails(t *testing.T) {
	scope := chain.NewScope(map[string]any{"tag": "t", "nested": map[string]any{"a": "b"}})
	scope.Record("p1", nil, map[string]any{"product": map[string]any{"id": "p-1"}, "tags": []any{"a"}})
	for _, text := range []string{"x-${p1.product}", "x-${p1.tags} y", "v ${vars.nested}"} {
		if got, err := scope.ResolveValue(text); err == nil {
			t.Errorf("%q resolved to %q; a message, list or map has no text form", text, got)
		}
	}
	if got, err := scope.ResolveValue("x-${p1.product.id}-${vars.tag}"); err != nil || got != "x-p-1-t" {
		t.Errorf("scalars inside text resolve: %v %v", got, err)
	}
}
