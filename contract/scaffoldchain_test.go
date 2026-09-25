package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

const repeatedProducerOverlay = `apiVersion: shrt/contract/v1
domain: shop
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        required: [sku]
        fields:
            sku:
                value: sku-${vars.tag}
        exports:
            product: the product
        status: draft
    shop.catalog.v1.StockService/AddStock:
        summary: adds stock
        required: [id_product]
        fields:
            id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
        exports:
            qty_on_hand: stock after
        status: draft
    shop.customers.v1.CustomerService/CreateCustomer:
        summary: adds a customer
        required: [NONE]
        fields:
            email:
                value: cust-${vars.tag}@example.test
        exports:
            customer: the customer
        status: draft
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines]
        fields:
            id_customer:
                from: shop.customers.v1.CustomerService/CreateCustomer->customer.id_customer
            lines.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
            lines.qty:
                value: "2"
        exports:
            order: the order
        status: draft
`

func TestChainNewScaffoldsARepeatedProducerThatCanRun(t *testing.T) {
	cat := catalogtest.Shop()
	lib := shopLibrary(t, repeatedProducerOverlay)
	refs := []string{"shop.catalog.v1.ProductService/CreateProduct", "shop.catalog.v1.ProductService/CreateProduct",
		"shop.catalog.v1.StockService/AddStock", "shop.customers.v1.CustomerService/CreateCustomer",
		"shop.orders.v1.OrderService/CreateOrder"}
	ids := []string{"create_product", "create_product_2", "add_stock", "create_customer", "create_order"}
	raw, notes, err := contract.ScaffoldChain("css", "", refs, ids, lib, cat)
	if err != nil {
		t.Fatal(err)
	}
	var c chain.Chain
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if c.Vars["tag"] == nil {
		t.Fatalf("a chain reading ${vars.tag} must declare it:\n%s", raw)
	}
	first, _ := c.Step("create_product")
	second, _ := c.Step("create_product_2")
	if first.Body["sku"] == second.Body["sku"] {
		t.Fatalf("two creates of the same rpc must not send the same unique value:\n%s", raw)
	}
	order, _ := c.Step("create_order")
	lines := orderLines(t, order.Body)
	if len(lines) != 2 || lines[0]["id_product"] == lines[1]["id_product"] {
		t.Fatalf("the two order lines should use the two products:\n%s", raw)
	}
	stock, _ := c.Step("add_stock")
	if stock.Body["id_product"] != "${create_product.product.id_product}" {
		t.Fatalf("each producer should be read, the first one by the first reader:\n%s", raw)
	}
	for _, st := range c.Steps {
		if len(st.Export) > 0 {
			t.Fatalf("step %s exports a value no step reads:\n%s", st.ID, raw)
		}
	}
	if !strings.Contains(strings.Join(notes, "\n"), "step create_product_2: another CreateProduct step is read by AddStock") {
		t.Fatalf("the scaffold should say the second product is not stocked: %v", notes)
	}
}
