package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

const twoProductsOverlay = `apiVersion: shrt/contract/v1
domain: catalog
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        required: [sku]
        fields:
            sku:
                value: SKU-A
            price_minor:
                value: "250"
        aliases:
            b:
                note: a second product at another price
                fields:
                    sku:
                        value: SKU-B
                    price_minor:
                        value: "400"
        status: draft
`

const twoLineOrderOverlay = `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines]
        fields:
            id_customer:
                from: shop.customers.v1.CustomerService/CreateCustomer->customer.id_customer
            lines.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
            lines.qty:
                value: "3"
            lines.1.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct@b->product.id_product
            lines.1.qty:
                value: "2"
        status: draft
`

func TestPlanDoesNotCallADeliberatelyWiredPlainStepADuplicate(t *testing.T) {
	cat := catalogtest.Shop()
	lib := shopLibrary(t, twoProductsOverlay, shopCustomersOverlay, twoLineOrderOverlay)
	plan, err := contract.BuildPlan(shopCreateOrder, lib, cat, "two-lines")
	if err != nil {
		t.Fatal(err)
	}
	if anyNote(plan.Notes, "likely a duplicate") {
		t.Fatalf("line 0 names the plain CreateProduct and line 1 names @b, with a different sku and price: "+
			"two products, not a duplicate: %v", plan.Notes)
	}
}

func TestPlanStillNotesAPlainStepIdenticalToAnAlias(t *testing.T) {
	cat := catalogtest.Shop()
	same := strings.Replace(strings.Replace(twoProductsOverlay, "value: SKU-B", "value: SKU-A", 1),
		`value: "400"`, `value: "250"`, 1)
	lib := shopLibrary(t, same, shopCustomersOverlay, twoLineOrderOverlay)
	plan, err := contract.BuildPlan(shopCreateOrder, lib, cat, "two-lines")
	if err != nil {
		t.Fatal(err)
	}
	if !anyNote(plan.Notes, "likely a duplicate") {
		t.Fatalf("@b sends exactly what the plain step sends, so one of them is a duplicate: %v", plan.Notes)
	}
}
