package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestASuggestedPathDropsAnIndexIntoAMessageAndKeepsOneIntoAList(t *testing.T) {
	cat := catalogtest.Shop()
	for _, tc := range []struct{ rpc, path, want string }{
		{"shop.catalog.v1.ProductService/CreateProduct", "products.0.id_product", ` (did you mean "product.id_product"?)`},
		{"shop.catalog.v1.ProductService/ListProducts", "product.0.id_product", ` (did you mean "products.0.id_product"?)`},
	} {
		m, err := cat.Lookup(tc.rpc)
		if err != nil {
			t.Fatal(err)
		}
		if got := chain.NearResponsePath(m, tc.path); got != tc.want {
			t.Fatalf("%s %s: got %q, want %q", tc.rpc, tc.path, got, tc.want)
		}
	}
}
