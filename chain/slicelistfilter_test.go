package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestSliceKeepsACreateAListReadFiltersByThroughAVar(t *testing.T) {
	ok := []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}
	c := &chain.Chain{Name: "catalog-listproducts", Vars: map[string]any{"tag": "t"}, Steps: []*chain.Step{
		{ID: "create_product", Call: "shop.v1.ProductService/CreateProduct", Body: map[string]any{"sku": "sku-${vars.tag}-c"}, Expect: ok},
		{ID: "create_product_prefix_case", Call: "shop.v1.ProductService/CreateProduct", Body: map[string]any{"sku": "SKU-${vars.tag}-case"}, Expect: ok},
		{ID: "create_customer", Call: "shop.v1.CustomerService/CreateCustomer", Body: map[string]any{"name": "fixed"}, Expect: ok},
		{ID: "list_products", Call: "shop.v1.ProductService/ListProducts", Body: map[string]any{"sku_prefix": "sku-${vars.tag}-"},
			Expect: []chain.Expectation{{Path: "products.0.id_product", Equals: "${create_product.product.id_product}"}, {Path: "products.1", Exists: new(false)}}},
		{ID: "list_products_without_token", Call: "shop.v1.ProductService/ListProducts", Body: map[string]any{"sku_prefix": "sku-${vars.tag}-"},
			SkipAuth: true, Expect: []chain.Expectation{{Path: "transport.code", Equals: "unauthenticated"}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	res, err := chain.Slice(c, "list_products", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]chain.Keep{}
	for _, k := range res.Kept {
		kept[k.ID] = k
	}
	k, ok2 := kept["create_product_prefix_case"]
	if !ok2 || !strings.Contains(k.Reason, "sku_prefix") {
		t.Fatalf("a create whose sku comes from the var the list filters by shapes the list and must be kept: %+v", res.Kept)
	}
	if _, in := kept["create_customer"]; in {
		t.Fatalf("a write sharing no filter var with the list is still left out: %+v", res.Kept)
	}
	probe, err := chain.Slice(c, "list_products_without_token", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.Kept) != 1 {
		t.Fatalf("an auth probe is refused before it lists anything, so it keeps no fixture: %+v", probe.Kept)
	}
}
