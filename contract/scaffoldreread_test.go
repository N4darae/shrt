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
		"get_product":   "${create_product.product.id_product}",
		"get_product_2": "${create_product_2.product.id_product}",
		"get_product_3": "${create_product.product.id_product}",
	}
	for id, ref := range want {
		st, _ := c.Step(id)
		if st.Body["id_product"] != ref {
			t.Fatalf("%s must read %s, got %v:\n%s", id, ref, st.Body["id_product"], raw)
		}
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "step get_product_3: reads create_product again after add_stock, as get_product did; to read create_product_2 instead") {
		t.Fatalf("the re-read must be named with its alternative, got:\n%s", joined)
	}
	if strings.Contains(joined, "get_product_2: reads") {
		t.Fatalf("a read with no write before it is not a re-read, got:\n%s", joined)
	}
}
