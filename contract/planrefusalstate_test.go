package contract_test

import (
	"strings"
	"testing"
)

func idAt(t *testing.T, ids []string, id string) int {
	t.Helper()
	for i, x := range ids {
		if x == id {
			return i
		}
	}
	t.Fatalf("no step %s among %s", id, strings.Join(ids, ", "))
	return -1
}

func TestPlanForAnInsufficiencyRefusalProvesTheRefusedWriteChangedNothing(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "ConfirmOrder")
	short := planStep(t, p, "create_order_for_insufficient_stock")
	if got := bodyAt(t, short, "lines.0.qty"); got != "100000" {
		t.Fatalf("the first line asks for far more than any stock, got %s:\n%s", got, text)
	}
	if got := bodyAt(t, short, "lines.1.qty"); got != bodyAt(t, planStep(t, p, "create_order"), "lines.1.qty") {
		t.Fatalf("the other line keeps a quantity that is in stock, got %s:\n%s", got, text)
	}
	last := planStep(t, p, "create_order_for_insufficient_stock_last_item")
	if got := bodyAt(t, last, "lines.1.qty"); got != "100000" {
		t.Fatalf("a second probe puts the shortage on the last line, which a backend checking only the first misses, got %s:\n%s", got, text)
	}
	planStep(t, p, "fetch_order_after_confirm_order_insufficient_stock_last_item")
	refused := planStep(t, p, "confirm_order_insufficient_stock")
	if got := bodyAt(t, refused, "id_order"); got != "${create_order_for_insufficient_stock.order.id_order}" {
		t.Fatalf("the refused confirm reads the short order, got %s:\n%s", got, text)
	}
	wantExpect(t, refused, "status.details.0.reason", "InsufficientStock")
	wantExpect(t, refused, "status.details.0.app_code", 1305)

	ids := stepIDs(p)
	at := idAt(t, ids, refused.ID)
	for _, pair := range [][3]string{
		{"get_product", "product.qty_on_hand", ""},
		{"get_product_2", "product.qty_on_hand", ""},
		{"fetch_order", "order.status", ""},
	} {
		before := pair[0] + "_before_" + refused.ID
		after := pair[0] + "_after_" + refused.ID
		if idAt(t, ids, before) > at || idAt(t, ids, after) < at {
			t.Fatalf("%s reads before the refusal and %s after it: %s", before, after, strings.Join(ids, ", "))
		}
		wantExpect(t, planStep(t, p, after), pair[1], "${"+before+"."+pair[1]+"}")
	}
	if !strings.Contains(notes, "confirm_order_insufficient_stock") || !strings.Contains(notes, "unchanged") {
		t.Fatalf("the plan says what the refusal probe proves: %s", notes)
	}
}
