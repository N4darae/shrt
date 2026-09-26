package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

const emptySameAsOverlay = `apiVersion: shrt/contract/v1
domain: catalog
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: Adds a product.
        required: [sku]
        fields:
            sku:
                note: unique across all products, fresh per run
        status: draft
    shop.catalog.v1.ProductService/GetProduct:
        summary: Reads one product.
        required: [id_product]
        fields:
            id_product:
                same_as: shop.catalog.v1.ProductService/CreateProduct->sku
        status: draft
`

func TestARequiredFieldReadingAnEmptySameAsVarCountsAsUnfilled(t *testing.T) {
	cat := catalogtest.Shop()
	lib := shopLibrary(t, emptySameAsOverlay)
	plan, err := contract.BuildPlan(shopGetProduct, lib, cat, "p")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Chain.Vars["create_product_sku"] != "" {
		t.Fatalf("the fixture must leave the shared var empty, vars = %v", plan.Chain.Vars)
	}
	if got := plan.UnfilledCount(); got != 2 {
		t.Fatalf("sku on create_product and id_product on get_product are required and both send the empty "+
			"${vars.create_product_sku}, so the header must count 2, got %d; notes: %v", got, plan.Notes)
	}
	errors := 0
	for _, i := range contract.LintChain(plan.Chain, cat, contract.ChainLintOptions{Library: lib}) {
		if i.IsError() && strings.Contains(i.Message, "vars:") && strings.Contains(i.Message, "empty") {
			errors++
		}
	}
	if errors != 2 {
		t.Fatalf("the header says chain lint errors on each; lint reported %d", errors)
	}
}
