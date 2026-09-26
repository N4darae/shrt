package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestAMessageReferencedInAHeaderIsRefusedUpFront(t *testing.T) {
	cat := catalogtest.Shop()
	for _, value := range []string{"${cp.product}", "p ${cp.product} q", "${prod}"} {
		c := createProductNamed("n")
		c.Steps[1].Headers = map[string]string{"X-Prod": value}
		if !lintErrorNaming(c, "X-Prod") {
			t.Errorf("X-Prod: %q sends a message as a header and lint did not error: %+v", value, chain.Lint(c, cat))
		}
		if p := c.ResponseRefProblems(cat); len(p) == 0 || !strings.Contains(strings.Join(p, " "), "header X-Prod") {
			t.Errorf("X-Prod: %q must be refused before anything is sent, got %v", value, p)
		}
	}
	c := createProductNamed("n")
	c.Steps[1].Headers = map[string]string{"X-Prod": "${cp.product.sku}"}
	if p := c.ResponseRefProblems(cat); len(p) != 0 {
		t.Errorf("a scalar in a header is fine, got %v", p)
	}
}

func TestAMapOrListVarInsideTextIsRefusedUpFront(t *testing.T) {
	cat := catalogtest.Shop()
	vars := map[string]any{"obj": map[string]any{"a": "b"}, "tags": []any{"x"}}
	for _, name := range []string{"n ${vars.obj}", "n ${vars.tags} y"} {
		c := createProductNamed(name)
		c.Vars = vars
		if !lintErrorNaming(c, "${vars.") {
			t.Errorf("name: %q interpolates a map or list var into text and lint did not error: %+v", name, chain.Lint(c, cat))
		}
		if p := c.VarStructureProblems(vars); len(p) == 0 {
			t.Errorf("name: %q must be refused before anything is sent", name)
		}
	}
	c := createProductNamed("n")
	c.Steps[1].Headers = map[string]string{"X-Obj": "${vars.obj}"}
	if p := c.VarStructureProblems(vars); len(p) == 0 || !strings.Contains(p[0], "header X-Obj") {
		t.Errorf("a map var as a whole header value has no text form either: %v", p)
	}
	for _, name := range []string{"n ${vars.obj.a}", "${vars.obj}"} {
		c := createProductNamed(name)
		if p := c.VarStructureProblems(vars); len(p) != 0 {
			t.Errorf("name: %q is a scalar in text or a whole value, got %v", name, p)
		}
	}
}
