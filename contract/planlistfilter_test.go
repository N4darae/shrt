package contract_test

import (
	"strings"
	"testing"
)

func TestPlanForAListWithAStatusFilterPutsFixturesInEachReachableStateAndFiltersOnEach(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "ListOrders")
	ids := stepIDs(p)
	list := idAt(t, ids, "list_orders")
	wantExists(t, planStep(t, p, "list_orders"), "orders.3", false)

	byState := map[string]string{}
	for _, id := range ids[list+1:] {
		st := planStep(t, p, id)
		switch {
		case strings.HasPrefix(id, "confirm_order"):
			byState["CONFIRMED"] = bodyAt(t, st, "id_order")
			wantExpect(t, st, "order.status", "ORDER_STATUS_CONFIRMED")
		case strings.HasPrefix(id, "cancel_order"):
			byState["CANCELLED"] = bodyAt(t, st, "id_order")
			wantExpect(t, st, "order.status", "ORDER_STATUS_CANCELLED")
		}
	}
	if byState["CONFIRMED"] == "" || byState["CANCELLED"] == "" || byState["CONFIRMED"] == byState["CANCELLED"] {
		t.Fatalf("after the unfiltered list, one fixture is confirmed and another cancelled: %v\n%s", byState, text)
	}
	for state, ref := range byState {
		filtered := planStep(t, p, "list_orders_"+strings.ToLower(state))
		if got := bodyAt(t, filtered, "status"); got != "ORDER_STATUS_"+state {
			t.Fatalf("%s filters on %s, got %s", filtered.ID, state, got)
		}
		wantExpect(t, filtered, "orders.0.id_order", ref)
		wantExpect(t, filtered, "orders.0.status", "ORDER_STATUS_"+state)
		wantExists(t, filtered, "orders.1", false)
	}
	pending := planStep(t, p, "list_orders_pending")
	wantExpect(t, pending, "orders.0.id_order", "${create_order.order.id_order}")
	if !strings.Contains(notes, "list_orders_confirmed") {
		t.Fatalf("the plan says what the filtered lists prove: %s", notes)
	}
}

func TestPlanForAPerParentListAddsAnotherParentWhoseItemsMustNotAppear(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "ListOrders")
	other := planStep(t, p, "create_customer_other")
	if bodyAt(t, other, "email") == bodyAt(t, planStep(t, p, "create_customer"), "email") {
		t.Fatalf("the other customer has its own unique email:\n%s", text)
	}
	item := planStep(t, p, "create_order_other_customer")
	if got := bodyAt(t, item, "id_customer"); got != "${create_customer_other.customer.id_customer}" {
		t.Fatalf("the other customer's order belongs to it, got %s:\n%s", got, text)
	}
	ids := stepIDs(p)
	if idAt(t, ids, item.ID) > idAt(t, ids, "list_orders") {
		t.Fatalf("the other customer's order exists before the list is read:\n%s", strings.Join(ids, ", "))
	}
}

func TestPlanForAPrefixListAddsItemsContainingThePrefixElsewhereAndInAnotherCase(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "ListProducts")
	prefix := bodyAt(t, planStep(t, p, "list_products"), "sku_prefix")
	inside := bodyAt(t, planStep(t, p, "create_product_prefix_inside"), "sku")
	if !strings.Contains(inside, prefix) || strings.HasPrefix(inside, prefix) {
		t.Fatalf("the inside fixture contains the prefix but does not start with it: prefix %s, sku %s\n%s", prefix, inside, text)
	}
	cased := bodyAt(t, planStep(t, p, "create_product_prefix_case"), "sku")
	if !strings.HasPrefix(strings.ToLower(cased), strings.ToLower(prefix)) || strings.HasPrefix(cased, prefix) {
		t.Fatalf("the case fixture starts with the prefix in another letter case: prefix %s, sku %s\n%s", prefix, cased, text)
	}
	wantExists(t, planStep(t, p, "list_products"), "products.4", false)
}
