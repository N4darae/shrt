package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestAReferenceWithArithmeticIsToldReferencesDoNone(t *testing.T) {
	c := &chain.Chain{Name: "arith", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A"}},
		{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${create.id}"},
			Expect: []chain.Expectation{{Path: "name", Equals: "${create.name + 3}"}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if strings.Contains(i.Message, "create.name + 3") {
			if !strings.Contains(i.Message, chain.NoArithmetic) || strings.Contains(i.Message, "not a field") {
				t.Fatalf("want the no-arithmetic message, got %s", i.Message)
			}
			return
		}
	}
	t.Fatal("the arithmetic reference was not reported")
}
