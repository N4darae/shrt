package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func listOverlay(summary string) string {
	return `apiVersion: shrt/contract/v1
domain: catalog
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        required: [sku, name]
        fields:
            sku:
                value: sku-${vars.tag}-${uuid}
            name:
                value: Widget
            price_minor:
                value: "250"
        status: draft
    shop.catalog.v1.ProductService/ListProducts:
        summary: ` + summary + `
        required: [NONE]
        fields:
            sku_prefix:
                same_as: shop.catalog.v1.ProductService/CreateProduct->sku
        status: draft
`
}

func TestPlanForAListGivesThreeFixturesWhoseSortKeysDisagree(t *testing.T) {
	lib := libraryFrom(t, listOverlay("lists products whose sku starts with a prefix, sorted by sku"))
	p, err := contract.BuildPlan("shop.catalog.v1.ProductService/ListProducts", lib, catalogtest.Shop(), "list")
	if err != nil {
		t.Fatal(err)
	}
	creates := map[string]map[string]any{}
	for _, st := range p.Chain.Steps {
		if strings.HasSuffix(st.Call, "/CreateProduct") {
			creates[st.ID] = st.Body
		}
	}
	if len(creates) != 3 {
		t.Fatalf("a list order is only discriminating with three items, got %d creates", len(creates))
	}
	order := func(field string, less func(a, b any) bool) string {
		ids := []string{"create_product", "create_product_2", "create_product_3"}
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				if less(creates[ids[j]][field], creates[ids[i]][field]) {
					ids[i], ids[j] = ids[j], ids[i]
				}
			}
		}
		return strings.Join(ids, ",")
	}
	text := func(a, b any) bool { return a.(string) < b.(string) }
	num := func(a, b any) bool {
		return len(a.(string)) < len(b.(string)) || (len(a.(string)) == len(b.(string)) && a.(string) < b.(string))
	}
	byName, byPrice := order("name", text), order("price_minor", num)
	orders := map[string]string{"name": byName, "price_minor": byPrice, "creation": "create_product,create_product_2,create_product_3",
		"sku": "create_product,create_product_3,create_product_2"}
	seen := map[string]string{}
	for k, o := range orders {
		if other, dup := seen[o]; dup {
			t.Fatalf("%s and %s sort the fixtures the same way (%s), so an order check could not tell them apart: %v", k, other, o, creates)
		}
		seen[o] = k
	}
	raw, err := p.YAML()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"sku: ${steps.create_product.request.sku}-b",
		"sku: ${steps.create_product.request.sku}-a",
		"- path: products.0.id_product\n          equals: ${create_product.product.id_product}",
		"- path: products.1.id_product\n          equals: ${create_product_3.product.id_product}",
		"- path: products.2.id_product\n          equals: ${create_product_2.product.id_product}",
		"- path: products.3\n          exists: false",
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("want %q in the plan:\n%s", want, raw)
		}
	}
}

func TestChainNewWithTwoCreatesFeedingAListMakesThreeThatSortApart(t *testing.T) {
	lib := libraryFrom(t, listOverlay("lists products whose sku starts with a prefix, sorted by sku"))
	raw, notes, err := contract.ScaffoldChain("lp", "", []string{"CreateProduct", "CreateProduct", "ListProducts"},
		[]string{"create_product", "create_product_2", "list_products"}, lib, catalogtest.Shop())
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Count(text, "call: shop.catalog.v1.ProductService/CreateProduct") != 3 {
		t.Fatalf("two creates cannot tell a sort key apart from creation order; chain new adds a third:\n%s", text)
	}
	if !strings.Contains(text, "sku_prefix: ${steps.create_product.request.sku}") || !strings.Contains(text, "- path: products.2.id_product") {
		t.Fatalf("the list reads the prefix all three share and asserts their order:\n%s\n%v", text, notes)
	}
}

func TestPlanForAListWithNoStatedOrderAssertsNoPosition(t *testing.T) {
	lib := libraryFrom(t, listOverlay("lists products whose sku starts with a prefix"))
	p, err := contract.BuildPlan("shop.catalog.v1.ProductService/ListProducts", lib, catalogtest.Shop(), "list")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := p.YAML()
	if strings.Contains(string(raw), "products.0.id_product") {
		t.Fatalf("the contract promises no order, so no position may be asserted:\n%s", raw)
	}
	if !strings.Contains(strings.Join(p.Notes, "\n"), "states no order") {
		t.Fatalf("the plan says how to have the order asserted: %v", p.Notes)
	}
}
