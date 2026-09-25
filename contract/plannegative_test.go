package contract_test

import (
	"strings"
	"testing"
)

func TestPlanProbesANegativeValueBelowAStatedMinimumAndProvesNothingMoved(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "AddStock")
	neg := planStep(t, p, "add_stock_qty_negative")
	if got := bodyAt(t, neg, "qty"); got != "-1" {
		t.Fatalf("a negative quantity reaches a sign bug that zero does not, got %s:\n%s", got, text)
	}
	wantExpect(t, neg, "status.details.0.reason", "InvalidQty")
	before := planStep(t, p, "get_product_before_add_stock_qty_negative")
	after := planStep(t, p, "get_product_after_add_stock_qty_negative")
	wantExpect(t, after, "product.qty_on_hand", "${"+before.ID+".product.qty_on_hand}")
	if !strings.Contains(notes, "add_stock_qty_negative") {
		t.Fatalf("the plan names the negative probe:\n%s", notes)
	}
	p, text, _ = shopDemoPlan(t, "CreateProduct")
	if got := bodyAt(t, planStep(t, p, "create_product_price_minor_negative"), "price_minor"); got != "-1" {
		t.Fatalf("price_minor is probed negative too, got %s:\n%s", got, text)
	}
}
