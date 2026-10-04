package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestChainNewWithoutContractsWiresIDsFromEarlierSteps(t *testing.T) {
	cat := catalogtest.Shop()
	refs := []string{"shop.orders.v1.OrderService/ConfirmOrder", "shop.catalog.v1.ProductService/CreateProduct",
		"shop.customers.v1.CustomerService/CreateCustomer", "shop.orders.v1.OrderService/CreateOrder",
		"shop.orders.v1.OrderService/CancelOrder"}
	ids := []string{"confirm_order", "create_product", "create_customer", "create_order", "cancel_order"}
	c, raw, _ := scaffolded(t, "bare", refs, ids, nil, cat)
	order, _ := c.Step("create_order")
	if order.Body["id_customer"] != "${create_customer.customer.id_customer}" {
		t.Fatalf("id_customer should read the earlier CreateCustomer:\n%s", raw)
	}
	if lines := orderLines(t, order.Body); len(lines) == 0 || lines[0]["id_product"] != "${create_product.product.id_product}" {
		t.Fatalf("lines.id_product should read the earlier CreateProduct:\n%s", raw)
	}
	if cancel, _ := c.Step("cancel_order"); cancel.Body["id_order"] != "${create_order.order.id_order}" {
		t.Fatalf("id_order should read the earlier CreateOrder:\n%s", raw)
	}
	if confirm, _ := c.Step("confirm_order"); confirm.Body["id_order"] != "" {
		t.Fatalf("a producer that runs later must not be wired:\n%s", raw)
	}
}
