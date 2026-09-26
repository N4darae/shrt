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

func TestAVarWithArithmeticLintsAsRunRefusesIt(t *testing.T) {
	c := &chain.Chain{Name: "arith", Vars: map[string]any{"stock": "3"}, Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "${vars.stock+5}"}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if strings.Contains(i.Message, "stock+5") {
			if i.Message != "${vars.stock+5} "+chain.NoArithmetic || i.Severity != chain.SeverityError {
				t.Fatalf("want the no-arithmetic error, got %s %s", i.Severity, i.Message)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("the arithmetic var was not reported")
	}
}
