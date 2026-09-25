package contract_test

import (
	"strings"
	"testing"
)

func TestPlanProbesEachIDTakingRPCWithAnIDNoRecordHas(t *testing.T) {
	for _, c := range []struct {
		target, step, field, ref, reason string
	}{
		{"GetProduct", "get_product_unknown_id_product", "id_product", "${create_product.product.id_product}", "ProductNotFound"},
		{"GetCustomer", "get_customer_unknown_id_customer", "id_customer", "${create_customer.customer.id_customer}", "CustomerNotFound"},
		{"AddStock", "add_stock_unknown_id_product", "id_product", "${create_product.product.id_product}", "ProductNotFound"},
		{"FetchOrder", "fetch_order_unknown_id_order", "id_order", "${create_order.order.id_order}", "OrderNotFound"},
	} {
		p, text, notes := shopDemoPlan(t, c.target)
		probe := planStep(t, p, c.step)
		if got := bodyAt(t, probe, c.field); got != c.ref+"-unknown" {
			t.Fatalf("%s: the probe sends a real id made unknown, got %s:\n%s", c.target, got, text)
		}
		wantExpect(t, probe, "status.details.0.reason", c.reason)
		if !strings.Contains(notes, c.step) {
			t.Fatalf("%s: the plan names the unknown-id probe:\n%s", c.target, notes)
		}
	}
	p, text, _ := shopDemoPlan(t, "ListOrders")
	if _, ok := p.Chain.Step("list_orders_unknown_id_customer"); ok {
		t.Fatalf("ListOrders declares no not-found failure for id_customer (checked_by: none), so no probe is guessed:\n%s", text)
	}
}
