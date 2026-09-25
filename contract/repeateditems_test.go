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
		if s.Call == shopCreateOrder {
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
	if first["id_product"] != second["id_product"] {
		t.Fatalf("a from: reference must stay the same in both items: %v vs %v", first["id_product"], second["id_product"])
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
