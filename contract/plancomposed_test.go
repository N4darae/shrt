package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestPlanCancelsAConfirmedOrderAndAssertsTheStockComesBack(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CancelOrder")
	ids := stepIDs(p)
	fixture := "create_order_for_cancel_order_after_confirmed"
	move := planStep(t, p, "confirm_order_before_cancel_order_after_confirmed")
	if !strings.HasSuffix(move.Call, "/ConfirmOrder") || bodyAt(t, move, "id_order") != "${"+fixture+".order.id_order}" {
		t.Fatalf("the fresh order is confirmed first: %+v\n%s", move, text)
	}
	cancel := planStep(t, p, "cancel_order_after_confirmed")
	if got := bodyAt(t, cancel, "id_order"); got != "${"+fixture+".order.id_order}" {
		t.Fatalf("the cancel acts on the confirmed order, got %s", got)
	}
	wantExpect(t, cancel, "order.status", "ORDER_STATUS_CANCELLED")
	wantExpect(t, planStep(t, p, "fetch_order_after_cancel_order_after_confirmed"), "order.status", "ORDER_STATUS_CANCELLED")
	for _, reader := range []string{"get_product", "get_product_2"} {
		before := reader + "_before_" + move.ID
		after := reader + "_after_" + cancel.ID
		if idAt(t, ids, before) > idAt(t, ids, move.ID) || idAt(t, ids, after) < idAt(t, ids, cancel.ID) {
			t.Fatalf("%s reads before the confirm and %s after the cancel: %s", before, after, strings.Join(ids, ", "))
		}
		wantExpect(t, planStep(t, p, after), "product.qty_on_hand", "${"+before+".product.qty_on_hand}")
	}
	if !strings.Contains(notes, "back to the reads taken before") {
		t.Fatalf("the plan says what the composition proves: %s", notes)
	}

	p, text, _ = shopDemoPlan(t, "ConfirmOrder")
	if _, ok := p.Chain.Step("confirm_order_after_cancelled"); ok {
		t.Fatalf("a confirm of a cancelled order is a declared refusal, not a composition:\n%s", text)
	}
}

func TestPlanSaysEachNoteOnce(t *testing.T) {
	p, _ := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.orders.v1.OrderService/ConfirmOrder"]; c != nil {
			c.Needs = append(c.Needs, "shop.catalog.v1.StockService/AddStockBatch")
		}
	}, "CancelOrder")
	seen := map[string]bool{}
	for _, n := range p.Notes {
		if seen[n] {
			t.Fatalf("note repeated: %s", n)
		}
		seen[n] = true
	}
	if !strings.Contains(strings.Join(p.Notes, "\n"), "would move a fixture to CONFIRMED") {
		t.Fatalf("the plan says why no fixture is confirmed: %v", p.Notes)
	}
}
