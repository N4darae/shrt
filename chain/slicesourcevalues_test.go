package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAFailureWithAnotherWantAndGotIsNotTheSameFailure(t *testing.T) {
	source := chain.Verdict{Status: "failed", ErrorCode: "SUCCESS", Expect: []chain.ExpectResult{
		{Path: "order.total_minor", Rule: "equals", Want: 3697, Got: 1995},
	}}
	replay := chain.Verdict{Status: "failed", ErrorCode: "SUCCESS", Expect: []chain.ExpectResult{
		{Path: "order.total_minor", Rule: "equals", Want: 4548, Got: 6250},
	}}
	diffs := chain.CompareVerdictsMasking(source, replay, nil)
	if len(diffs) == 0 {
		t.Fatal("the slice failed the expectation with another want and another got, so it is not the source's failure")
	}
	if !strings.Contains(diffs[0], "3697") || !strings.Contains(diffs[0], "6250") {
		t.Fatalf("the difference must show both sides' values: %v", diffs)
	}
}

func TestAVerifiedClosureSliceRunsWithTheSourceRunsVars(t *testing.T) {
	c := &chain.Chain{
		Name: "orders",
		Vars: map[string]any{"price": 1250, "tag": "orders"},
		Steps: []*chain.Step{
			{ID: "create_product", Call: "ProductService/CreateProduct", Body: map[string]any{"sku": "sku-${vars.tag}", "price_minor": "${vars.price}"}},
			{ID: "create_order", Call: "OrderService/CreateOrder", Body: map[string]any{"id_product": "${create_product.product.id_product}"}},
		},
	}
	res, err := chain.Slice(c, "create_order", chain.SliceOptions{
		RunID: "r1", RunVars: map[string]any{"price": 399, "tag": "old"}, RunVarsAsDefaults: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Chain.Vars["price"]; got != 399 {
		t.Fatalf("a slice verified against run r1 must send what r1 sent, price=399; it has price=%v", got)
	}
	if got := res.Chain.Vars["tag"]; got != "orders" {
		t.Fatalf("a fresh var is not taken from the run, whose value the backend has seen: tag=%v", got)
	}
}
