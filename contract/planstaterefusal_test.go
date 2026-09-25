package contract_test

import (
	"strings"
	"testing"
)

func TestPlanDrivesTheEntityIntoEachStateItsContractRefuses(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "ConfirmOrder")
	ids := stepIDs(p)
	for _, tc := range []struct {
		refused, move, call, fixture string
		code                         int
		reason, state                string
	}{
		{"confirm_order_when_confirmed", "confirm_order_to_confirmed_for_confirm_order", "ConfirmOrder", "create_order_for_confirm_order_confirmed", 1303, "OrderAlreadyConfirmed", "ORDER_STATUS_CONFIRMED"},
		{"confirm_order_when_cancelled", "cancel_order_to_cancelled_for_confirm_order", "CancelOrder", "create_order_for_confirm_order_cancelled", 1304, "OrderCancelled", "ORDER_STATUS_CANCELLED"},
	} {
		refused := planStep(t, p, tc.refused)
		wantExpect(t, refused, "status.details.0.app_code", tc.code)
		wantExpect(t, refused, "status.details.0.reason", tc.reason)
		for _, e := range refused.Expect {
			if e.Path == "order" && e.Exists != nil {
				t.Fatalf("%s must not assert the order absent, a refusal may carry it: %+v", tc.refused, refused.Expect)
			}
		}
		if got := bodyAt(t, refused, "id_order"); got != "${"+tc.fixture+".order.id_order}" {
			t.Fatalf("%s acts on a fresh order of its own, got %s:\n%s", tc.refused, got, text)
		}
		move := planStep(t, p, tc.move)
		if !strings.HasSuffix(move.Call, "/"+tc.call) || bodyAt(t, move, "id_order") != "${"+tc.fixture+".order.id_order}" {
			t.Fatalf("%s moves %s with %s: %+v", tc.move, tc.fixture, tc.call, move)
		}
		wantExpect(t, move, "order.status", tc.state)
		if idAt(t, ids, tc.move) > idAt(t, ids, tc.refused) {
			t.Fatalf("%s runs before %s: %s", tc.move, tc.refused, strings.Join(ids, ", "))
		}
		after := planStep(t, p, "fetch_order_after_"+tc.refused)
		wantExpect(t, after, "order.status", "${fetch_order_before_"+tc.refused+".order.status}")
		wantExpect(t, planStep(t, p, "get_product_after_"+tc.refused), "product.qty_on_hand", "${get_product_before_"+tc.refused+".product.qty_on_hand}")
	}
	unknown := planStep(t, p, "confirm_order_unknown_id_order")
	if got := bodyAt(t, unknown, "id_order"); !realIDMadeUnknown(got, "${create_order.order.id_order}") {
		t.Fatalf("the unknown-order probe sends an id nothing created, got %q", got)
	}
	wantExpect(t, unknown, "status.details.0.app_code", 1302)
	if !strings.Contains(notes, "1303 OrderAlreadyConfirmed") || !strings.Contains(notes, "1302 OrderNotFound") {
		t.Fatalf("the plan names the refusals it probes: %s", notes)
	}

	p, text, _ = shopDemoPlan(t, "CancelOrder")
	wantExpect(t, planStep(t, p, "cancel_order_when_cancelled"), "status.details.0.app_code", 1304)
	wantExpect(t, planStep(t, p, "cancel_order_unknown_id_order"), "status.details.0.app_code", 1302)
	if _, ok := p.Chain.Step("cancel_order_when_confirmed"); ok {
		t.Fatalf("CancelOrder declares no refusal for a CONFIRMED order:\n%s", text)
	}
}

func TestPlanRefusesEachUnknownReferenceWithTheCodeItsContractNames(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "CreateOrder")
	customer := planStep(t, p, "create_order_unknown_id_customer")
	wantExpect(t, customer, "status.details.0.app_code", 1301)
	if got := bodyAt(t, customer, "id_customer"); !realIDMadeUnknown(got, "${create_customer.customer.id_customer}") {
		t.Fatalf("id_customer names no customer, got %s", got)
	}
	product := planStep(t, p, "create_order_unknown_id_product")
	wantExpect(t, product, "status.details.0.app_code", 1204)
	if got := bodyAt(t, product, "lines.1.id_product"); !realIDMadeUnknown(got, "${create_product_2.product.id_product}") {
		t.Fatalf("the last line names no product, got %s:\n%s", got, text)
	}
	if got := bodyAt(t, product, "lines.0.id_product"); !strings.Contains(got, "${") || strings.HasSuffix(got, "-unknown") {
		t.Fatalf("the first line keeps a real product, got %s", got)
	}

	p, _, _ = shopDemoPlan(t, "GetCustomer")
	wantExpect(t, planStep(t, p, "get_customer_unknown_id_customer"), "status.details.0.app_code", 1102)

	p, text, _ = shopDemoPlan(t, "AddStockBatch")
	if _, ok := p.Chain.Step("add_stock_batch_unknown_id_product"); ok {
		t.Fatalf("a failure reported on that line only is not a refusal of the whole call:\n%s", text)
	}
}
