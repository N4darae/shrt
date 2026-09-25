package chain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func orderChain(t *testing.T, aName, aPrice, bName, bPrice string) *chain.Chain {
	t.Helper()
	path := filepath.Join(t.TempDir(), "order.yaml")
	raw := `apiVersion: shrt/v1
name: order
vars:
    tag: t
steps:
    - id: create_b
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-b", name: "` + bName + `", price_minor: "` + bPrice + `"}
    - id: create_a
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-a", name: "` + aName + `", price_minor: "` + aPrice + `"}
    - id: list
      call: ProductService/ListProducts
      body: {sku_prefix: "cp-${vars.tag}-"}
      expect:
        - path: products.0.sku
          equals: ${steps.create_a.request.sku}
        - path: products.1.id_product
          equals: ${create_b.product.id_product}
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func orderHints(c *chain.Chain) string {
	out := []string{}
	for _, i := range chain.LintWith(c, catalogtest.Shop(), chain.LintOptions{Hints: true}) {
		if i.Kind == chain.KindIndistinctOrder {
			out = append(out, i.Message)
		}
	}
	return strings.Join(out, "\n")
}

func TestLintHintsWhenEveryCandidateKeySortsTheItemsAlike(t *testing.T) {
	got := orderHints(orderChain(t, "Anchor", "1", "Bolt", "1999"))
	if !strings.Contains(got, "name, price_minor, sku") {
		t.Fatalf("sku, name and price_minor all put create_a before create_b, so the order check cannot tell which sorts: %q", got)
	}
}

func TestLintIsQuietWhenOnlyOneKeyMatchesTheAssertedOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "order3.yaml")
	raw := `apiVersion: shrt/v1
name: order3
vars:
    tag: t
steps:
    - id: create_1
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-a", name: "Beta", price_minor: "300"}
    - id: create_2
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-c", name: "Alpha", price_minor: "100"}
    - id: create_3
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-b", name: "Gamma", price_minor: "200"}
    - id: list
      call: ProductService/ListProducts
      body: {sku_prefix: "cp-${vars.tag}-"}
      expect:
        - path: products.0.id_product
          equals: ${create_1.product.id_product}
        - path: products.1.id_product
          equals: ${create_3.product.id_product}
        - path: products.2.id_product
          equals: ${create_2.product.id_product}
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := orderHints(c); got != "" {
		t.Fatalf("only sku puts the three items in the asserted order; name, price and creation order all disagree: %q", got)
	}
}
