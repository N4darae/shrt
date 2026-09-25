package contract_test

import (
	"strings"
	"testing"
)

func TestPlanForAReservingWriteConfirmsExactlyTheStockOnHandAndReadsZeroAfter(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "ConfirmOrder")
	order := planStep(t, p, "create_order_for_confirm_order_exact_stock")
	if got, want := bodyAt(t, order, "lines.0.qty"), bodyAt(t, planStep(t, p, "add_stock"), "qty"); got != want {
		t.Fatalf("the first line asks for exactly the %s units add_stock put on hand, got %s:\n%s", want, got, text)
	}
	if got, want := bodyAt(t, order, "lines.1.qty"), bodyAt(t, planStep(t, p, "add_stock_2"), "qty"); got != want {
		t.Fatalf("the last line asks for exactly the %s units add_stock_2 put on hand, got %s:\n%s", want, got, text)
	}
	act := planStep(t, p, "confirm_order_exact_stock")
	wantExpect(t, act, "status.code", "SUCCESS")
	if got := bodyAt(t, act, "id_order"); got != "${create_order_for_confirm_order_exact_stock.order.id_order}" {
		t.Fatalf("the exact probe confirms its own order, got %s:\n%s", got, text)
	}
	wantExpect(t, planStep(t, p, "get_product_after_confirm_order_exact_stock"), "product.qty_on_hand", 0)
	wantExpect(t, planStep(t, p, "get_product_2_after_confirm_order_exact_stock"), "product.qty_on_hand", 0)
	if strings.Contains(bodyAt(t, order, "lines.0.id_product"), "${create_product.") {
		t.Fatalf("the exact probe runs on fixtures of its own, so the shortage probes cannot drain them:\n%s", text)
	}
	if !strings.Contains(notes, "confirm_order_exact_stock asks for exactly the stock") {
		t.Fatalf("the plan says why it confirms the exact stock:\n%s", notes)
	}
}
