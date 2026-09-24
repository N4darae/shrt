package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestARequestPathTheEarlierRequestDoesNotDeclareIsALintError(t *testing.T) {
	c := misspeltRefChain("${steps.create.request.nmae}")
	found := false
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if i.Kind == chain.KindDeadRef && strings.Contains(i.Message, `"nmae"`) {
			if !i.IsError() {
				t.Fatalf("the descriptor says create's request has no nmae, so the run dies on it: %+v", i)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("${steps.create.request.nmae} names no field of CreateRequest and lint said nothing")
	}
	if p := c.ResponseRefProblems(catalogtest.New()); len(p) == 0 || !strings.Contains(strings.Join(p, " "), "nmae") {
		t.Fatalf("the up-front check must refuse the request path too, got %v", p)
	}
}

func TestARequestPathTheEarlierRequestDeclaresIsNotReported(t *testing.T) {
	c := misspeltRefChain("${steps.create.request.name}")
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if i.Kind == chain.KindDeadRef {
			t.Fatalf("name is a field of CreateRequest: %+v", i)
		}
	}
	if p := c.ResponseRefProblems(catalogtest.New()); len(p) != 0 {
		t.Fatalf("name is a field of CreateRequest, got %v", p)
	}
}
