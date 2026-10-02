package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

func TestChainNewReadRepeatedAfterAWriteReadsTheSameResourceAgain(t *testing.T) {
	cat := catalogtest.Shop()
	lib := shopLibrary(t, repeatedProducerOverlay)
	refs := []string{"shop.catalog.v1.ProductService/CreateProduct", "shop.catalog.v1.ProductService/CreateProduct",
		"shop.catalog.v1.ProductService/GetProduct", "shop.catalog.v1.ProductService/GetProduct",
		"shop.catalog.v1.StockService/AddStock", "shop.catalog.v1.ProductService/GetProduct"}
	ids := []string{"create_product", "create_product_2", "get_product", "get_product_2", "add_stock", "get_product_3"}
	raw, notes, err := contract.ScaffoldChain("rr", "", refs, ids, lib, cat)
	if err != nil {
		t.Fatal(err)
	}
	var c chain.Chain
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"get_product":                 "${create_product.product.id_product}",
		"get_product_2":               "${create_product_2.product.id_product}",
		"get_product_after_add_stock": "${create_product.product.id_product}",
	}
	for id, ref := range want {
		st, _ := c.Step(id)
		if st.Body["id_product"] != ref {
			t.Fatalf("%s must read %s, got %v:\n%s", id, ref, st.Body["id_product"], raw)
		}
	}
	if joined := strings.Join(notes, "\n"); strings.Contains(joined, "again after") {
		t.Fatalf("the id names the write a re-read follows, so no note repeats it, got:\n%s", joined)
	}
}
