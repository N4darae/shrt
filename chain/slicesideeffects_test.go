package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func confirmShortageChain() *chain.Chain {
	line := func(product, qty string) map[string]any {
		return map[string]any{"id_product": "${" + product + ".product.id_product}", "qty": qty}
	}
	return &chain.Chain{Name: "orders-confirm", Steps: []*chain.Step{
		{ID: "create_product", Call: "ProductService/CreateProduct", Body: map[string]any{"sku": "a"}},
		{ID: "create_product_2", Call: "ProductService/CreateProduct", Body: map[string]any{"sku": "b"}},
		{ID: "create_product_3", Call: "ProductService/CreateProduct", Body: map[string]any{"sku": "c"}},
		{ID: "add_stock", Call: "StockService/AddStock", Body: map[string]any{"id_product": "${create_product.product.id_product}", "qty": "10"}},
		{ID: "add_stock_2", Call: "StockService/AddStock", Body: map[string]any{"id_product": "${create_product_2.product.id_product}", "qty": "11"}},
		{ID: "add_stock_3", Call: "StockService/AddStock", Body: map[string]any{"id_product": "${create_product_3.product.id_product}", "qty": "1"}},
		{ID: "create_customer", Call: "CustomerService/CreateCustomer", Body: map[string]any{"email": "c@example.test"}},
		{ID: "create_order", Call: "OrderService/CreateOrder", Body: map[string]any{
			"id_customer": "${create_customer.customer.id_customer}",
			"lines":       []any{line("create_product", "3"), line("create_product_2", "4")},
		}},
		{ID: "confirm_order", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id_order": "${create_order.order.id_order}"}},
		{ID: "create_order_3", Call: "OrderService/CreateOrder", Body: map[string]any{
			"id_customer": "${create_customer.customer.id_customer}",
			"lines":       []any{line("create_product_3", "1")},
		}},
		{ID: "confirm_order_3", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id_order": "${create_order_3.order.id_order}"}},
		{ID: "create_order_other", Call: "OrderService/CreateOrder", Body: map[string]any{
			"id_customer": "${create_customer.customer.id_customer}",
			"lines":       []any{line("create_product", "1")},
		}},
		{ID: "create_order_last_item", Call: "OrderService/CreateOrder", Body: map[string]any{
			"id_customer": "${create_customer.customer.id_customer}",
			"lines":       []any{line("create_product", "3"), line("create_product_2", "100000")},
		}},
		{ID: "confirm_order_last_item", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id_order": "${create_order_last_item.order.id_order}"}},
	}}
}

func confirmNeedsStock(rpc string) []chain.Prereq {
	if rpc == "OrderService/ConfirmOrder" {
		return []chain.Prereq{{RPC: "StockService/AddStock", Edge: "needs"}}
	}
	return nil
}

func TestSliceKeepsTheNeededCallForEveryEntityTheTargetReachesThroughItsOrder(t *testing.T) {
	res, err := chain.Slice(confirmShortageChain(), "confirm_order_last_item", chain.SliceOptions{Prereqs: confirmNeedsStock})
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]chain.Keep{}
	for _, k := range res.Kept {
		kept[k.ID] = k
	}
	if _, ok := kept["add_stock"]; !ok {
		t.Fatalf("ConfirmOrder needs AddStock and the order it confirms has a line on each product, so the stock of both is kept: %+v", res.Kept)
	}
	if _, ok := kept["add_stock_2"]; !ok {
		t.Fatalf("the second product's stock is kept too: %+v", res.Kept)
	}
	if _, ok := kept["add_stock_3"]; ok {
		t.Fatalf("the third product is on no line of the confirmed order: %+v", res.Kept)
	}
}

func TestSliceDoesNotKeepAWriteThatExpectsToBeRefusedForItsSideEffects(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	c := confirmShortageChain()
	for _, s := range c.Steps {
		if s.ID == "confirm_order" {
			s.Expect = []chain.Expectation{{Path: "status.code", NotEqual: "SUCCESS"}}
		}
	}
	res, err := chain.Slice(c, "confirm_order_last_item", chain.SliceOptions{Prereqs: confirmNeedsStock})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range res.Kept {
		if k.ID == "confirm_order" || k.ID == "create_order" {
			t.Fatalf("a confirm expected to be refused changes nothing, so it is not kept for its side effects: %+v", res.Kept)
		}
	}
}

func TestSliceKeepsAnEarlierWriteThatChangesTheStateTheTargetNeeds(t *testing.T) {
	res, err := chain.Slice(confirmShortageChain(), "confirm_order_last_item", chain.SliceOptions{Prereqs: confirmNeedsStock})
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]chain.Keep{}
	for _, k := range res.Kept {
		kept[k.ID] = k
	}
	k, ok := kept["confirm_order"]
	if !ok {
		t.Fatalf("an earlier ConfirmOrder reserves stock of the same products the target's order lines name: it is kept: %+v", res.Kept)
	}
	if _, ok := kept["create_order"]; !ok {
		t.Fatalf("the kept confirm_order reads create_order: %+v", res.Kept)
	}
	if k.Reason == "" || k.Kind != chain.KeepSideEffect {
		t.Fatalf("the reason names the side effect: %+v", k)
	}
	for _, id := range []string{"confirm_order_3", "create_order_3", "create_order_other", "add_stock_3"} {
		if _, ok := kept[id]; ok {
			t.Fatalf("%s changes no state the target needs (another product, or a create that needs no stock): %+v", id, res.Kept)
		}
	}
}
