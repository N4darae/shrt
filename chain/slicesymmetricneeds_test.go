package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestSliceKeepsTheNeededCallForEveryEntityTheTargetReads(t *testing.T) {
	c := &chain.Chain{Name: "orders", Steps: []*chain.Step{
		{ID: "create_product", Call: "ProductService/CreateProduct", Body: map[string]any{"sku": "a"}},
		{ID: "create_product_2", Call: "ProductService/CreateProduct", Body: map[string]any{"sku": "b"}},
		{ID: "create_product_3", Call: "ProductService/CreateProduct", Body: map[string]any{"sku": "c"}},
		{ID: "add_stock", Call: "StockService/AddStock", Body: map[string]any{"id_product": "${create_product.product.id_product}", "qty": "5"}},
		{ID: "add_stock_2", Call: "StockService/AddStock", Body: map[string]any{"id_product": "${create_product_2.product.id_product}", "qty": "10"}},
		{ID: "add_stock_3", Call: "StockService/AddStock", Body: map[string]any{"id_product": "${create_product_3.product.id_product}", "qty": "1"}},
		{ID: "create_customer", Call: "CustomerService/CreateCustomer", Body: map[string]any{"email": "c@example.test"}},
		{ID: "create_order", Call: "OrderService/CreateOrder", Body: map[string]any{
			"id_customer": "${create_customer.customer.id_customer}",
			"lines": []any{
				map[string]any{"id_product": "${create_product.product.id_product}", "qty": "2"},
				map[string]any{"id_product": "${create_product_2.product.id_product}", "qty": "3"},
			},
		}},
	}}
	prereqs := func(rpc string) []chain.Prereq {
		if rpc == "OrderService/CreateOrder" {
			return []chain.Prereq{{RPC: "StockService/AddStock", Edge: "needs"}}
		}
		return nil
	}
	res, err := chain.Slice(c, "create_order", chain.SliceOptions{Prereqs: prereqs})
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]bool{}
	for _, k := range res.Kept {
		kept[k.ID] = true
	}
	if !kept["add_stock"] || !kept["add_stock_2"] {
		t.Fatalf("CreateOrder needs AddStock and its lines read both products, so the stock of both is kept, not only the last call's: %+v", res.Kept)
	}
	if kept["add_stock_3"] || kept["create_product_3"] {
		t.Fatalf("the third product is on no line of the order, so its stock is not needed: %+v", res.Kept)
	}
}
