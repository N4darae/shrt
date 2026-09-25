package contract_test

import (
	"strings"
	"testing"
)

func TestStatusFilterMovesRunOnResourcesOfTheirOwn(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateOrder", "ConfirmOrder", "CancelOrder", "FetchOrder", "ListOrders")
	ids := stepIDs(p)
	moved := planStep(t, p, "create_order_3")
	for i, want := range []string{"create_product_for_filter", "create_product_2_for_filter"} {
		got := bodyAt(t, moved, "lines."+string(rune('0'+i))+".id_product")
		if got != "${"+want+".product.id_product}" {
			t.Fatalf("create_order_3, which confirm_order_3 moves, orders a product of its own, got %s\n%s", got, text)
		}
		if idAt(t, ids, want) > idAt(t, ids, "create_order_3") {
			t.Fatalf("%s is created before create_order_3: %s", want, strings.Join(ids, ", "))
		}
	}
	if idAt(t, ids, "add_stock_for_filter") > idAt(t, ids, "create_order_3") || idAt(t, ids, "add_stock_2_for_filter") > idAt(t, ids, "create_order_3") {
		t.Fatalf("the filter's products are stocked before the order that confirm_order_3 confirms: %s", strings.Join(ids, ", "))
	}
	if got := bodyAt(t, moved, "id_customer"); got != "${create_customer.customer.id_customer}" {
		t.Fatalf("the list's scope stays the customer it lists, got %s", got)
	}
	for _, id := range []string{"create_order", "create_order_2"} {
		if got := bodyAt(t, planStep(t, p, id), "lines.0.id_product"); got != "${create_product.product.id_product}" {
			t.Fatalf("%s, which no filter write moves, keeps the main path's product, got %s", id, got)
		}
	}
	wantExpect(t, planStep(t, p, "list_orders_confirmed"), "orders.0.id_order", "${create_order_3.order.id_order}")
	wantExpect(t, planStep(t, p, "get_product_after_confirm_order_3"), "product.qty_on_hand", 5-4)
	wantExpect(t, planStep(t, p, "get_product_2_after_confirm_order_3"), "product.qty_on_hand", 6-1)
	if !strings.Contains(notes, "filter (4, for create_order_3)") {
		t.Fatalf("the isolation note names the filter's fixtures:\n%s", notes)
	}
}
