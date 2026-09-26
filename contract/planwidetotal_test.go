package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestPlanForAPricedTotalSendsALineWhoseSumPasses32Bits(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateOrder")
	probe := planStep(t, p, "create_order_wide_total")
	prod := planStep(t, p, "create_product_for_create_order_wide_total")
	if got := bodyAt(t, prod, "price_minor"); got != "1500000000" {
		t.Fatalf("the probe's product is priced at 1500000000, got %s:\n%s", got, text)
	}
	if got := bodyAt(t, probe, "lines.0.id_product"); got != "${create_product_for_create_order_wide_total.product.id_product}" {
		t.Fatalf("the probe's line names its own product, got %s:\n%s", got, text)
	}
	if got := bodyAt(t, probe, "lines.0.qty"); got != "3" {
		t.Fatalf("3 at 1500000000 is past 2^32, got qty %s:\n%s", got, text)
	}
	wantExpect(t, probe, "order.total_minor", 4500000000)
	if !strings.Contains(notes, "create_order_wide_total sends one line of 3") {
		t.Fatalf("the plan says why it sends a wide total:\n%s", notes)
	}
}

func TestAWideTotalStaysWithinADeclaredPriceMaximum(t *testing.T) {
	p, text := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.catalog.v1.ProductService/CreateProduct"]; c != nil {
			c.Fields["price_minor"].Note = "price in cents, greater than zero and at most 100000000"
		}
	}, "CreateOrder")
	if got := bodyAt(t, planStep(t, p, "create_product_for_create_order_wide_total"), "price_minor"); got != "100000000" {
		t.Fatalf("the price stays at the declared maximum, got %s:\n%s", got, text)
	}
	probe := planStep(t, p, "create_order_wide_total")
	if got := bodyAt(t, probe, "lines.0.qty"); got != "43" {
		t.Fatalf("the quantity grows until the sum passes 2^32, got %s:\n%s", got, text)
	}
	wantExpect(t, probe, "order.total_minor", 4300000000)
}
