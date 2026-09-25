package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func cancelRestockChain() *chain.Chain {
	ok := []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}
	return &chain.Chain{Name: "cancel-restock", Steps: []*chain.Step{
		{ID: "create_product", Call: "shop.v1.ProductService/CreateProduct", Body: map[string]any{"sku": "a"}, Expect: ok},
		{ID: "create_product_2", Call: "shop.v1.ProductService/CreateProduct", Body: map[string]any{"sku": "b"}, Expect: ok},
		{ID: "add_stock", Call: "shop.v1.StockService/AddStock", Body: map[string]any{"id_product": "${create_product.product.id_product}"}, Expect: ok},
		{ID: "add_stock_2", Call: "shop.v1.StockService/AddStock", Body: map[string]any{"id_product": "${create_product_2.product.id_product}"}, Expect: ok},
		{ID: "create_customer", Call: "shop.v1.CustomerService/CreateCustomer", Expect: ok},
		{ID: "create_other_customer", Call: "shop.v1.CustomerService/CreateCustomer", Expect: ok},
		{ID: "create_order", Call: "shop.v1.OrderService/CreateOrder", Body: map[string]any{
			"id_customer": "${create_customer.customer.id_customer}",
			"lines": []any{
				map[string]any{"id_product": "${create_product.product.id_product}"},
				map[string]any{"id_product": "${create_product_2.product.id_product}"},
			},
		}, Expect: ok},
		{ID: "confirm_order", Call: "shop.v1.OrderService/ConfirmOrder", Body: map[string]any{"id_order": "${create_order.order.id_order}"}, Expect: ok},
		{ID: "cancel_order", Call: "shop.v1.OrderService/CancelOrder", Body: map[string]any{"id_order": "${confirm_order.order.id_order}"}, Expect: ok},
		{ID: "get_product_2_after_cancel", Call: "shop.v1.ProductService/GetProduct", Body: map[string]any{"id_product": "${create_product_2.product.id_product}"},
			Expect: []chain.Expectation{{Path: "product.qty_on_hand", Equals: "${add_stock_2.qty_on_hand}"}}},
	}}
}

func TestSliceKeepsAWriteThatChangesTheStateAKeptReadReads(t *testing.T) {
	res, err := chain.Slice(cancelRestockChain(), "get_product_2_after_cancel", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]chain.Keep{}
	for _, k := range res.Kept {
		kept[k.ID] = k
	}
	for _, id := range []string{"create_order", "confirm_order", "cancel_order", "create_customer", "add_stock_2", "create_product_2"} {
		if _, ok := kept[id]; !ok {
			t.Fatalf("%s acts on the product the target reads, so it stays: kept %+v", id, res.Kept)
		}
	}
	if k := kept["cancel_order"]; k.Kind != chain.KeepSideEffect || !strings.Contains(k.Reason, "create_product_2") || !strings.Contains(k.Reason, "get_product_2_after_cancel") {
		t.Fatalf("the reason names the entity and the step that reads it: %+v", k)
	}
	for _, id := range []string{"create_other_customer", "add_stock"} {
		if _, ok := kept[id]; ok {
			t.Fatalf("%s acts on nothing the target reads: kept %+v", id, res.Kept)
		}
	}
}

func TestSliceInPinModeLeavesARecordedStateWriteToTheRun(t *testing.T) {
	c := cancelRestockChain()
	res, err := chain.Slice(c, "get_product_2_after_cancel", chain.SliceOptions{
		Mode:      chain.SliceModePin,
		Value:     func(ref string) (any, bool) { return "pinned-" + ref, true },
		Performed: func(string) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range res.Kept {
		if k.ID == "cancel_order" {
			t.Fatalf("under -mode pin the run already did the cancel, and re-sending it would change the state again: %+v", res.Kept)
		}
	}
}
