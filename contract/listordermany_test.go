package contract_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func prefixListOverlay(sku string) string {
	return `apiVersion: shrt/contract/v1
domain: catalog
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        required: [sku, name]
        fields:
            sku:
                value: ` + sku + `
            name:
                value: Widget
            price_minor:
                value: "250"
        status: draft
    shop.catalog.v1.ProductService/ListProducts:
        summary: lists products whose sku starts with a prefix, sorted by sku
        required: [NONE]
        fields:
            sku_prefix:
                value: sku-${vars.tag}-
        status: draft
`
}

func manyCreatesThenList(t *testing.T, sku string) (string, []string) {
	t.Helper()
	lib := libraryFrom(t, prefixListOverlay(sku))
	refs, ids := []string{}, []string{}
	for i := 1; i <= 11; i++ {
		refs = append(refs, "CreateProduct")
		id := "create_product"
		if i > 1 {
			id += "_" + strconv.Itoa(i)
		}
		ids = append(ids, id)
	}
	raw, notes, err := contract.ScaffoldChain("many", "", append(refs, "ListProducts"), append(ids, "list_products"), lib, catalogtest.Shop())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw), notes
}

func TestChainNewAssertsEveryFixtureOfAListInSortOrder(t *testing.T) {
	text, notes := manyCreatesThenList(t, "sku-${vars.tag}-")
	list := text[strings.Index(text, "id: list_products"):]
	for _, want := range []string{
		"- path: products.0.id_product\n          equals: ${create_product_10.product.id_product}",
		"- path: products.7.id_product\n          equals: ${create_product.product.id_product}",
		"- path: products.10.id_product\n          equals: ${create_product_2.product.id_product}",
		"- path: products.11\n          exists: false",
	} {
		if !strings.Contains(list, want) {
			t.Errorf("want %q in:\n%s\n%v", want, list, notes)
		}
	}
	if strings.Contains(list, "products.3\n          exists: false") {
		t.Errorf("eleven fixtures are listed, not three:\n%s", list)
	}
}

func TestChainNewAssertsOnlyMembershipWhenTheExtraFixturesCannotBeOrdered(t *testing.T) {
	text, notes := manyCreatesThenList(t, "sku-${vars.tag}-${uuid}")
	list := text[strings.Index(text, "id: list_products"):]
	if strings.Contains(list, ".id_product\n          equals:") {
		t.Errorf("no position can be known for fixtures whose sku ends in a uuid:\n%s", list)
	}
	if strings.Count(list, "includes:") != 11 || !strings.Contains(list, "- path: products.11\n          exists: false") {
		t.Errorf("every fixture is asserted by id and the count is exact:\n%s", list)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "no position is asserted, only that each fixture is in it by id") {
		t.Errorf("the note says why no order is asserted: %v", notes)
	}
}
