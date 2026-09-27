package contract_test

import (
	"fmt"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func TestPlanScaffoldsTwoDistinctItemsForARepeatedMessageInput(t *testing.T) {
	plan, err := contract.BuildPlan(shopCreateOrder, cancelFlowLibrary(t), catalogtest.Shop(), "two")
	if err != nil {
		t.Fatal(err)
	}
	var order *chain.Step
	for _, s := range plan.Chain.Steps {
		if s.Call == shopCreateOrder && order == nil {
			order = s
		}
	}
	if order == nil {
		t.Fatalf("no CreateOrder step in %v", plan.Order)
	}
	lines, ok := order.Body["lines"].([]any)
	if !ok || len(lines) < 2 {
		t.Fatalf("lines = %#v, want at least two items so per-item logic is exercised", order.Body["lines"])
	}
	first, second := lines[0].(map[string]any), lines[1].(map[string]any)
	if fmt.Sprint(first) == fmt.Sprint(second) {
		t.Fatalf("the two items are identical (%v), want different values", first)
	}
	if first["qty"] != "2" || second["qty"] != "3" {
		t.Fatalf("qty = %v and %v, want the contract's 2 and a distinct 3", first["qty"], second["qty"])
	}
	if first["id_product"] != "${create_product.product.id_product}" || second["id_product"] != "${create_product_2.product.id_product}" {
		t.Fatalf("the second item must read a second producer, not the first one's product: %v vs %v", first["id_product"], second["id_product"])
	}
	if !anyNote(plan.Notes, "lines") || !anyNote(plan.Notes, "two items") {
		t.Fatalf("the plan must say why lines carries two items: %v", plan.Notes)
	}
}

func TestSingleItemRepeatsNamesAFieldNoChainSendsTwice(t *testing.T) {
	cat := catalogtest.Shop()
	one := &chain.Chain{Name: "one", Steps: []*chain.Step{{
		ID:   "create_order",
		Call: shopCreateOrder,
		Body: map[string]any{"lines": []any{map[string]any{"id_product": "p", "qty": "3"}}},
	}}}
	got := contract.SingleItemRepeats([]*chain.Chain{one}, cat)
	if len(got) != 1 || got[0].RPC != shopCreateOrder || got[0].Field != "lines" || got[0].Most != 1 {
		t.Fatalf("got %+v, want CreateOrder lines sent with at most one item", got)
	}
	if len(got[0].Chains) != 1 || got[0].Chains[0] != "one" {
		t.Fatalf("chains = %v, want [one]", got[0].Chains)
	}

	two := &chain.Chain{Name: "two", Steps: []*chain.Step{{
		ID:   "create_order",
		Call: "OrderService/CreateOrder",
		Body: map[string]any{"lines": []any{map[string]any{"qty": "1"}, map[string]any{"qty": "2"}}},
	}}}
	if got := contract.SingleItemRepeats([]*chain.Chain{one, two}, cat); len(got) != 0 {
		t.Fatalf("a chain sends two lines, so nothing is missing: %+v", got)
	}
}

func TestSingleItemRepeatsNamesItemsThatAllPointAtOneResource(t *testing.T) {
	cat := catalogtest.Shop()
	same := &chain.Chain{Name: "same", Steps: []*chain.Step{
		{ID: "create_product", Call: shopCreateProduct, Body: map[string]any{"sku": "s"}},
		{ID: "create_order", Call: shopCreateOrder, Body: map[string]any{"lines": []any{
			map[string]any{"id_product": "${create_product.product.id_product}", "qty": "2"},
			map[string]any{"id_product": "${create_product.product.id_product}", "qty": "3"},
		}}},
	}}
	got := contract.SingleItemRepeats([]*chain.Chain{same}, cat)
	if len(got) != 1 || got[0].Field != "lines" || !got[0].SameResource || got[0].Most != 2 {
		t.Fatalf("got %+v, want CreateOrder lines reported as items that all point at the same resource", got)
	}
	if got[0].Resource != "${create_product.product.id_product}" {
		t.Fatalf("resource = %q, want the reference both items share", got[0].Resource)
	}

	distinct := &chain.Chain{Name: "distinct", Steps: []*chain.Step{
		{ID: "a", Call: shopCreateProduct, Body: map[string]any{"sku": "a"}},
		{ID: "b", Call: shopCreateProduct, Body: map[string]any{"sku": "b"}},
		{ID: "create_order", Call: shopCreateOrder, Body: map[string]any{"lines": []any{
			map[string]any{"id_product": "${a.product.id_product}", "qty": "2"},
			map[string]any{"id_product": "${b.product.id_product}", "qty": "3"},
		}}},
	}}
	if got := contract.SingleItemRepeats([]*chain.Chain{same, distinct}, cat); len(got) != 0 {
		t.Fatalf("a chain sends two lines for two products, so nothing is missing: %+v", got)
	}
	literal := &chain.Chain{Name: "literal", Steps: []*chain.Step{
		{ID: "create_order", Call: shopCreateOrder, Body: map[string]any{"lines": []any{
			map[string]any{"id_product": "p-1", "qty": "2"},
			map[string]any{"id_product": "p-1", "qty": "3"},
		}}},
	}}
	if got := contract.SingleItemRepeats([]*chain.Chain{literal}, cat); len(got) != 1 || !got[0].SameResource {
		t.Fatalf("two lines naming the same literal id point at one resource: %+v", got)
	}
}

func TestSingleItemRepeatsNamesAFieldThatNeverCarriesOneResourceTwice(t *testing.T) {
	cat := catalogtest.Shop()
	line := func(src, qty string) map[string]any {
		return map[string]any{"id_product": "${" + src + ".product.id_product}", "qty": qty}
	}
	distinct := &chain.Chain{Name: "distinct", Steps: []*chain.Step{
		{ID: "create_product", Call: shopCreateProduct, Body: map[string]any{"sku": "a"}},
		{ID: "create_product_2", Call: shopCreateProduct, Body: map[string]any{"sku": "b"}},
		{ID: "create_order", Call: shopCreateOrder, Body: map[string]any{"lines": []any{line("create_product", "2"), line("create_product_2", "3")}}},
		{ID: "create_order_refused", Call: shopCreateOrder, Body: map[string]any{"lines": []any{line("create_product", "0"), line("create_product", "0")}},
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "invalid_argument"}}},
	}}
	got := contract.SingleItemRepeats([]*chain.Chain{distinct}, cat)
	if len(got) != 1 || got[0].Field != "lines" || !got[0].NoRepeat || got[0].SameResource {
		t.Fatalf("got %+v, want CreateOrder lines named as never carrying one product twice (a refused step does not count)", got)
	}
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].status.code")
	refusedItem := &chain.Chain{Name: "refused_item", Steps: []*chain.Step{
		{ID: "create_product", Call: shopCreateProduct, Body: map[string]any{"sku": "d"}},
		{ID: "create_order", Call: shopCreateOrder, Body: map[string]any{"lines": []any{line("create_product", "2"), line("create_product", "0")}},
			Expect: []chain.Expectation{{Path: "results.1.status.code", Equals: "REJECTED"}}},
	}}
	if got := contract.SingleItemRepeats([]*chain.Chain{distinct, refusedItem}, cat); len(got) != 1 || !got[0].NoRepeat {
		t.Fatalf("got %+v, want lines still named: the second item on the product is refused, so nothing is applied twice", got)
	}
	twice := &chain.Chain{Name: "twice", Steps: []*chain.Step{
		{ID: "create_product", Call: shopCreateProduct, Body: map[string]any{"sku": "c"}},
		{ID: "create_order", Call: shopCreateOrder, Body: map[string]any{"lines": []any{line("create_product", "2"), line("create_product", "3")}}},
	}}
	if got := contract.SingleItemRepeats([]*chain.Chain{distinct, twice}, cat); len(got) != 0 {
		t.Fatalf("one chain sends distinct products and another one product twice, so nothing is missing: %+v", got)
	}
}
