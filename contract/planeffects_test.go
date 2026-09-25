package contract_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestPlanAssertsTheNumbersTheContractsState(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateOrder", "ConfirmOrder", "CancelOrder", "FetchOrder")
	wantExpect(t, planStep(t, p, "add_stock"), "qty_on_hand", 5)
	wantExpect(t, planStep(t, p, "add_stock_2"), "qty_on_hand", 6)
	wantExpect(t, planStep(t, p, "create_order"), "order.total_minor", 2*250+3*1250)
	wantExpect(t, planStep(t, p, "fetch_order"), "order.total_minor", 2*250+3*1250)
	wantExpect(t, planStep(t, p, "get_product_after_confirm_order"), "product.qty_on_hand", 5-2)
	wantExpect(t, planStep(t, p, "get_product_2_after_confirm_order"), "product.qty_on_hand", 6-3)
	if _, ok := p.Chain.Step("get_product_after_cancel_order"); ok {
		t.Fatalf("the level after the main cancel depends on the confirm before it, so it is left to the composed probe:\n%s", text)
	}
	wantExpect(t, planStep(t, p, "get_product_after_cancel_order_after_confirmed"), "product.qty_on_hand",
		"${get_product_before_confirm_order_before_cancel_order_after_confirmed.product.qty_on_hand}")
	for _, id := range []string{"get_product_before_confirm_order_insufficient_stock", "get_product_before_create_order_unknown_id_customer"} {
		for _, e := range planStep(t, p, id).Expect {
			if e.Path == "product.qty_on_hand" && e.Equals != nil && !strings.Contains(fmt.Sprint(e.Equals), "${") {
				t.Fatalf("%s follows no write that moves stock, so it asserts no level: %+v", id, e)
			}
		}
	}

	twice := planStep(t, p, "create_order_for_confirm_order_same_product_twice")
	if a, b := bodyAt(t, twice, "lines.0.id_product"), bodyAt(t, twice, "lines.1.id_product"); a != b || !strings.Contains(a, "_for_twice.") {
		t.Fatalf("both lines name one product of the probe's own: %s %s\n%s", a, b, text)
	}
	wantExpect(t, twice, "order.total_minor", 2*250)
	wantExpect(t, planStep(t, p, "confirm_order_same_product_twice"), "order.status", "ORDER_STATUS_CONFIRMED")
	wantExpect(t, planStep(t, p, "get_product_after_confirm_order_same_product_twice"), "product.qty_on_hand", 5-2)
	if !strings.Contains(notes, "assert numbers the plan works out") || !strings.Contains(notes, "Reserve stock for every line") ||
		strings.Contains(notes, "AddStockBatch") {
		t.Fatalf("the plan names the sentences it computed from, and only for rpcs it calls:\n%s", notes)
	}

	p, _, _ = shopDemoPlan(t, "AddStockBatch")
	wantExpect(t, planStep(t, p, "add_stock_batch"), "results.0.qty_on_hand", 3)
	wantExpect(t, planStep(t, p, "add_stock_batch"), "results.1.qty_on_hand", 4)
}

func TestPlanNamesWhatToDeclareWhenAContractStatesNoEffect(t *testing.T) {
	cat, _ := shopDemo(t)
	lib := shopDemoEdited(t, func(name, body string) string {
		if name != "orders.yaml" {
			return body
		}
		body = strings.Replace(body, "Reserve stock for every line and move", "Move", 1)
		body = strings.Replace(body, "priced at current product prices; does not touch stock.", "for a customer.", 1)
		return strings.Replace(body, ", total_minor is the priced sum", "", 1)
	})
	p, err := contract.BuildPlanFor([]string{"ConfirmOrder"}, lib, cat, "shopdemo")
	if err != nil {
		t.Fatal(err)
	}
	notes := strings.Join(p.Notes, "\n")
	for _, e := range planStep(t, p, "create_order").Expect {
		if e.Path == "order.total_minor" {
			t.Fatalf("no contract says how the total is computed, so none is asserted: %+v", e)
		}
	}
	for _, st := range p.Chain.Steps {
		for _, e := range st.Expect {
			if e.Path == "product.qty_on_hand" && e.Equals != nil && !strings.Contains(fmt.Sprint(e.Equals), "${") {
				t.Fatalf("no contract says what confirm does to stock, so %s asserts no level: %+v", st.ID, e)
			}
		}
	}
	if _, ok := p.Chain.Step("confirm_order_same_product_twice"); ok {
		t.Fatalf("without a stated reservation there is no per-line probe")
	}
	if !strings.Contains(notes, `ConfirmOrder touches what the plan tracks`) || !strings.Contains(notes, `"Reserve stock for every line" or "does not touch stock"`) ||
		!strings.Contains(notes, `CreateOrder touches what the plan tracks`) {
		t.Fatalf("the plan names what to declare:\n%s", notes)
	}
}
