package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

const pricedProducerOverlay = `apiVersion: shrt/contract/v1
domain: shop
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        required: [sku]
        fields:
            sku:
                value: sku-${vars.tag}
            price_minor:
                value: "250"
        exports:
            product: the product
        status: draft
    shop.catalog.v1.StockService/AddStock:
        summary: adds stock
        required: [id_product]
        fields:
            id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
            qty:
                value: "10"
        status: draft
    shop.customers.v1.CustomerService/CreateCustomer:
        summary: adds a customer
        required: [NONE]
        fields:
            email:
                value: cust-${vars.tag}@example.test
        status: draft
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines]
        needs: [shop.catalog.v1.StockService/AddStock]
        fields:
            id_customer:
                from: shop.customers.v1.CustomerService/CreateCustomer->customer.id_customer
            lines.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
            lines.qty:
                value: "2"
        status: draft
`

func stepIndex(c *chain.Chain, id string) int {
	for i, s := range c.Steps {
		if s.ID == id {
			return i
		}
	}
	return -1
}

func checkSecondProducer(t *testing.T, c *chain.Chain, notes []string) {
	t.Helper()
	order, ok := c.Step("create_order")
	if !ok {
		t.Fatalf("no create_order step")
	}
	lines := orderLines(t, order.Body)
	if len(lines) != 2 {
		t.Fatalf("lines = %v, want two items", lines)
	}
	if lines[0]["id_product"] != "${create_product.product.id_product}" ||
		lines[1]["id_product"] != "${create_product_2.product.id_product}" {
		t.Fatalf("the two items must point at two different products, got %v and %v", lines[0]["id_product"], lines[1]["id_product"])
	}
	first, _ := c.Step("create_product")
	second, ok := c.Step("create_product_2")
	if !ok {
		t.Fatalf("no second CreateProduct step: %v", c.Steps)
	}
	if second.Call != first.Call {
		t.Fatalf("create_product_2 calls %s, want %s", second.Call, first.Call)
	}
	if first.Body["sku"] == second.Body["sku"] {
		t.Fatalf("the second product must send its own sku, both send %v", first.Body["sku"])
	}
	if first.Body["price_minor"] == second.Body["price_minor"] {
		t.Fatalf("the second product must carry a different price, both send %v", first.Body["price_minor"])
	}
	stock2, ok := c.Step("add_stock_2")
	if !ok {
		t.Fatalf("the first product is stocked, so the second must be too: %v", c.Steps)
	}
	if stock2.Body["id_product"] != "${create_product_2.product.id_product}" {
		t.Fatalf("add_stock_2 must stock the second product, got %v", stock2.Body["id_product"])
	}
	if stepIndex(c, "create_product_2") > stepIndex(c, "add_stock_2") || stepIndex(c, "add_stock_2") > stepIndex(c, "create_order") {
		t.Fatalf("the second product and its stock must run before the order")
	}
	all := strings.Join(notes, "\n")
	if !strings.Contains(all, "create_product_2") {
		t.Fatalf("the notes must name the second producer: %v", notes)
	}
}

func TestPlanGivesTheSecondItemItsOwnProducer(t *testing.T) {
	plan, err := contract.BuildPlan(shopCreateOrder, shopLibrary(t, pricedProducerOverlay), catalogtest.Shop(), "two")
	if err != nil {
		t.Fatal(err)
	}
	checkSecondProducer(t, plan.Chain, plan.Notes)
	raw, err := plan.YAML()
	if err != nil {
		t.Fatal(err)
	}
	var c chain.Chain
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Step("create_product_2"); !ok {
		t.Fatalf("the written chain must carry the second producer:\n%s", raw)
	}
}

func TestChainNewGivesTheSecondItemItsOwnProducer(t *testing.T) {
	refs := []string{shopCreateProduct, shopAddStock, "shop.customers.v1.CustomerService/CreateCustomer", shopCreateOrder}
	ids := []string{"create_product", "add_stock", "create_customer", "create_order"}
	c, _, notes := scaffolded(t, "cn", refs, ids, shopLibrary(t, pricedProducerOverlay), catalogtest.Shop())
	checkSecondProducer(t, c, notes)
}

func TestChainNewKeepsTwoListedProducersAsTheyAre(t *testing.T) {
	refs := []string{shopCreateProduct, shopCreateProduct, "shop.customers.v1.CustomerService/CreateCustomer", shopCreateOrder}
	ids := []string{"create_product", "create_product_2", "create_customer", "create_order"}
	c, raw, _ := scaffolded(t, "cn", refs, ids, shopLibrary(t, pricedProducerOverlay), catalogtest.Shop())
	if len(c.Steps) != 4 {
		t.Fatalf("two listed producers already give the items distinct resources, want no extra step:\n%s", raw)
	}
}
