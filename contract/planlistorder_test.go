package contract_test

import (
	"fmt"
	"slices"
	"sort"
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

func TestPlanForAListGivesFixturesWhoseSortKeysDisagreeEitherWay(t *testing.T) {
	lib := libraryFrom(t, listOverlay("lists products whose sku starts with a prefix, sorted by sku"))
	p, err := contract.BuildPlan("shop.catalog.v1.ProductService/ListProducts", lib, catalogtest.Shop(), "list")
	if err != nil {
		t.Fatal(err)
	}
	creates := map[string]map[string]any{}
	for _, st := range p.Chain.Steps {
		if strings.HasSuffix(st.Call, "/CreateProduct") && !strings.Contains(st.ID, "_prefix_") && !strings.Contains(st.Description, " of 12 for ") {
			creates[st.ID] = st.Body
		}
	}
	if _, ok := p.Chain.Step("create_product_prefix_inside"); !ok {
		t.Fatalf("a prefix list also gets a fixture containing the prefix elsewhere, which it must not list")
	}
	ids := []string{"create_product", "create_product_2", "create_product_3", "create_product_4"}
	if len(creates) != len(ids) {
		t.Fatalf("sku, name, price_minor and creation need four items to sort apart either way, got %d creates", len(creates))
	}
	order := func(key func(id string) string, less func(a, b string) bool) []string {
		out := append([]string{}, ids...)
		sort.SliceStable(out, func(i, j int) bool { return less(key(out[i]), key(out[j])) })
		return out
	}
	field := func(name string) func(string) string {
		return func(id string) string { return creates[id][name].(string) }
	}
	text := func(a, b string) bool { return a < b }
	num := func(a, b string) bool { return len(a) < len(b) || len(a) == len(b) && a < b }
	skuSuffix := func(id string) string {
		_, suffix, _ := strings.Cut(creates[id]["sku"].(string), "}-")
		return suffix
	}
	orders := map[string][]string{"name": order(field("name"), text), "price_minor": order(field("price_minor"), num),
		"sku": order(skuSuffix, text), "creation": ids}
	for a, oa := range orders {
		for b, ob := range orders {
			rev := append([]string{}, ob...)
			slices.Reverse(rev)
			if a < b && (slices.Equal(oa, ob) || slices.Equal(oa, rev)) {
				t.Fatalf("%s and %s sort the fixtures the same way or in reverse (%v, %v), so an order check could not tell them apart: %v", a, b, oa, ob, creates)
			}
		}
	}
	raw, err := p.YAML()
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range orders["sku"] {
		if want := fmt.Sprintf("- path: products.%d.id_product\n          equals: ${%s.product.id_product}", i, id); !strings.Contains(string(raw), want) {
			t.Fatalf("want %q in the plan:\n%s", want, raw)
		}
	}
	if !strings.Contains(string(raw), "- path: products.4\n          exists: false") {
		t.Fatalf("the list holds exactly the four:\n%s", raw)
	}
}

func TestChainNewWithTwoCreatesFeedingAListMakesFourThatSortApart(t *testing.T) {
	lib := libraryFrom(t, listOverlay("lists products whose sku starts with a prefix, sorted by sku"))
	raw, notes, err := contract.ScaffoldChain("lp", "", []string{"CreateProduct", "CreateProduct", "ListProducts"},
		[]string{"create_product", "create_product_2", "list_products"}, lib, catalogtest.Shop())
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Count(text, "call: shop.catalog.v1.ProductService/CreateProduct") != 4 {
		t.Fatalf("two creates cannot tell sku, name, price_minor and creation order apart; chain new adds two:\n%s", text)
	}
	if !strings.Contains(text, "sku_prefix: ${steps.create_product.request.sku}") || !strings.Contains(text, "- path: products.3.id_product") {
		t.Fatalf("the list reads the prefix all four share and asserts their order:\n%s\n%v", text, notes)
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
