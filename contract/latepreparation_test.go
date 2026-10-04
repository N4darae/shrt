package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

var confirmNeedsStockOverlay = strings.Replace(pricedProducerOverlay, "        needs: [shop.catalog.v1.StockService/AddStock]\n", "", 1) + `    shop.orders.v1.OrderService/ConfirmOrder:
        summary: reserves stock for every line
        required: [NONE]
        needs: [shop.catalog.v1.StockService/AddStock]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.id_order
        status: draft
`

func TestPlanStocksTheSecondProductWhenStockIsNeededAfterTheOrder(t *testing.T) {
	plan, err := contract.BuildPlanFor([]string{shopCreateOrder, shopConfirmOrder}, shopLibrary(t, confirmNeedsStockOverlay), catalogtest.Shop(), "late")
	if err != nil {
		t.Fatal(err)
	}
	c := plan.Chain
	stock2, ok := c.Step("add_stock_2")
	if !ok {
		t.Fatalf("create_product is stocked for the confirm, so create_product_2 must be too: %v", stepIDs(plan))
	}
	if stock2.Body["id_product"] != "${create_product_2.product.id_product}" {
		t.Fatalf("add_stock_2 must stock the second product, got %v", stock2.Body["id_product"])
	}
	if stock2.Body["qty"] != "11" {
		t.Fatalf("add_stock_2 raises its numbers by one, got qty %v", stock2.Body["qty"])
	}
	if stepIndex(c, "add_stock") > stepIndex(c, "add_stock_2") || stepIndex(c, "add_stock_2") > stepIndex(c, "confirm_order") {
		t.Fatalf("add_stock_2 must follow add_stock and precede confirm_order")
	}
	if !strings.Contains(strings.Join(plan.Notes, "\n"), "add_stock_2") {
		t.Fatalf("a note names the added preparation: %v", plan.Notes)
	}
}

func TestPlanRaisesTheNumbersOfTheSecondPreparation(t *testing.T) {
	plan, err := contract.BuildPlan(shopCreateOrder, shopLibrary(t, pricedProducerOverlay), catalogtest.Shop(), "two")
	if err != nil {
		t.Fatal(err)
	}
	stock2, ok := plan.Chain.Step("add_stock_2")
	if !ok {
		t.Fatal("no add_stock_2")
	}
	if stock2.Body["qty"] != "11" {
		t.Fatalf("add_stock_2 raises its numbers by one as PLAYBOOK §3 says, got qty %v", stock2.Body["qty"])
	}
}
