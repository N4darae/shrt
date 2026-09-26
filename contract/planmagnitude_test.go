package contract_test

import (
	"strconv"
	"strings"
	"testing"
)

func TestPlanGivesTheSecondProducerAPriceOfAnotherMagnitudeAndAssertsItIsStored(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "CreateOrder")
	second := planStep(t, p, "create_product_2")
	price, err := strconv.Atoi(bodyAt(t, second, "price_minor"))
	if err != nil || price < 1000 {
		t.Fatalf("the second product's price is >= 1000, so arithmetic that only goes wrong on large amounts is reached, got %s:\n%s",
			bodyAt(t, second, "price_minor"), text)
	}
	wantExpect(t, second, "product.price_minor", "${steps.create_product_2.request.price_minor}")
	wantExpect(t, planStep(t, p, "create_product"), "product.price_minor", "${steps.create_product.request.price_minor}")
}

func TestPlanForACreateWithAStatedMinimumProbesTheBoundaryAndAMagnitude(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateProduct")
	min := planStep(t, p, "create_product_price_minor_min")
	if got := bodyAt(t, min, "price_minor"); got != "1" {
		t.Fatalf("the contract says price_minor must be greater than zero, so 1 is the boundary, got %s:\n%s", got, text)
	}
	wantExpect(t, min, "status.code", "SUCCESS")
	wantExpect(t, min, "product.price_minor", "${steps.create_product_price_minor_min.request.price_minor}")
	if bodyAt(t, min, "sku") == bodyAt(t, planStep(t, p, "create_product"), "sku") {
		t.Fatalf("a boundary probe is a new product, so its unique sku differs:\n%s", text)
	}
	large := planStep(t, p, "create_product_price_minor_large")
	if got := bodyAt(t, large, "price_minor"); got != "12345" {
		t.Fatalf("a large amount reaches arithmetic that small ones do not, got %s:\n%s", got, text)
	}
	wantExpect(t, large, "product.price_minor", "${steps.create_product_price_minor_large.request.price_minor}")
	below := planStep(t, p, "create_product_price_minor_below_min")
	if got := bodyAt(t, below, "price_minor"); got != "0" {
		t.Fatalf("one below the boundary is refused, got %s:\n%s", got, text)
	}
	wantExpect(t, below, "status.details.0.reason", "InvalidPrice")
	if !strings.Contains(notes, "create_product_price_minor_below_min") {
		t.Fatalf("the plan says where the boundary came from: %s", notes)
	}
}

func TestPlanForAQuantityWithAStatedMinimumRefusesZeroAndProvesStockUnchanged(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "AddStock")
	if got := bodyAt(t, planStep(t, p, "add_stock_qty_min"), "qty"); got != "1" {
		t.Fatalf("qty 1 is the boundary, got %s:\n%s", got, text)
	}
	below := planStep(t, p, "add_stock_qty_below_min")
	if got := bodyAt(t, below, "qty"); got != "0" {
		t.Fatalf("qty 0 is refused, got %s:\n%s", got, text)
	}
	wantExpect(t, below, "status.details.0.app_code", 1203)
	wantExpect(t, planStep(t, p, "get_product_after_add_stock_qty_below_min"), "product.qty_on_hand",
		"${get_product_before_add_stock_qty_below_min.product.qty_on_hand}")
	if _, ok := p.Chain.Step("add_stock_qty_large"); ok {
		t.Fatalf("a quantity is not probed large: a large quantity runs into stock rules, not arithmetic:\n%s", text)
	}
}

func TestPlanVariesNumbersInsideRepeatedItemsAcrossListFixtures(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "ListOrders")
	seen := map[string]bool{}
	for _, id := range []string{"create_order", "create_order_2", "create_order_3"} {
		qty := bodyAt(t, planStep(t, p, id), "lines.0.qty")
		if seen[qty] {
			t.Fatalf("three orders with the same lines are one fixture three times; vary qty, got %s twice:\n%s", qty, text)
		}
		seen[qty] = true
		if n, _ := strconv.Atoi(qty); n > 5 {
			t.Fatalf("a quantity grows by one per fixture so it stays within the stock the plan adds, got %s:\n%s", qty, text)
		}
	}
}
