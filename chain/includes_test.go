package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestIncludesHoldsWhenSomeItemMatchesEveryNamedField(t *testing.T) {
	resp := map[string]any{"products": []any{
		map[string]any{"id_product": "p-1", "sku": "a"},
		map[string]any{"id_product": "p-12", "sku": "b", "qty": "3"},
	}}
	cases := []struct {
		want any
		ok   bool
	}{
		{map[string]any{"id_product": "p-12"}, true},
		{map[string]any{"id_product": "p-12", "qty": 3}, true},
		{map[string]any{"id_product": "p-1", "sku": "b"}, false},
		{map[string]any{"id_product": "p-2"}, false},
	}
	for _, c := range cases {
		r := chain.Expectation{Path: "products", Includes: c.want}.Evaluate(resp)
		if r.Passed != c.ok || r.Rule != "includes" {
			t.Fatalf("includes %v: passed %v, want %v (%s)", c.want, r.Passed, c.ok, r)
		}
	}
	if r := (chain.Expectation{Path: "missing", Includes: map[string]any{"id_product": "p-1"}}).Evaluate(resp); r.Passed {
		t.Fatalf("an absent list includes nothing: %s", r)
	}
	if r := (chain.Expectation{Path: "products.0.sku", Includes: "a"}).Evaluate(resp); r.Passed {
		t.Fatalf("a scalar is not a list: %s", r)
	}
	tags := map[string]any{"tags": []any{"x", "y"}}
	if r := (chain.Expectation{Path: "tags", Includes: "y"}).Evaluate(tags); !r.Passed {
		t.Fatalf("a list of scalars includes its item: %s", r)
	}
}

func TestIncludesIsOneRuleAndResolvesItsReferences(t *testing.T) {
	c := &chain.Chain{Name: "inc", Steps: []*chain.Step{{
		ID:     "s",
		Call:   "Svc/List",
		Expect: []chain.Expectation{{Path: "products", Includes: map[string]any{"id_product": "${vars.id}"}}},
	}}, Vars: map[string]any{"id": "p-9"}}
	refs := c.Steps[0].Expect[0].References()
	if len(refs) != 1 || refs[0] != "vars.id" {
		t.Fatalf("the reference inside includes is seen: %v", refs)
	}
}
