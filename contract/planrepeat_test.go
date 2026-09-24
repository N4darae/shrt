package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

const shopGetProduct = "shop.catalog.v1.ProductService/GetProduct"

const beforeAfterOverlay = `apiVersion: shrt/contract/v1
domain: catalog
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: Adds a product.
        required: [sku]
        fields:
            sku:
                value: SKU-1
        status: draft
    shop.catalog.v1.ProductService/GetProduct:
        summary: Reads one product. Returns its stock.
        required: [id_product]
        fields:
            id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
        aliases:
            before:
                note: the read ahead of a stock change
            after: {}
        status: draft
    shop.catalog.v1.StockService/AddStock:
        summary: Adds stock.
        required: [id_product]
        fields:
            id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
            qty:
                value: "5"
        needs: [shop.catalog.v1.ProductService/GetProduct@before]
        status: draft
`

func TestPlanSaysWhenItMergesARepeatedTarget(t *testing.T) {
	lib := shopLibrary(t, beforeAfterOverlay)
	plan, err := contract.BuildPlanFor([]string{shopGetProduct, shopAddStock, shopGetProduct}, lib, catalogtest.Shop(), "p")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, node := range plan.Order {
		if node == shopGetProduct {
			calls++
		}
	}
	if calls != 1 {
		t.Fatalf("order = %v, want the plain GetProduct once", plan.Order)
	}
	found := ""
	for _, n := range plan.Notes {
		if strings.Contains(n, "GetProduct") && strings.Contains(n, "merged") {
			found = n
		}
	}
	if found == "" {
		t.Fatalf("the second GetProduct target was dropped without a note: %v", plan.Notes)
	}
	for _, want := range []string{"GetProduct@after", "after, before"} {
		if !strings.Contains(found, want) {
			t.Fatalf("the merge note must say how to alias the repeat (%q): %s", want, found)
		}
	}
}

func TestAnAliasedStepIsDescribedByItsAliasNote(t *testing.T) {
	lib := shopLibrary(t, beforeAfterOverlay)
	plan, err := contract.BuildPlanFor([]string{shopAddStock, shopGetProduct + "@after"}, lib, catalogtest.Shop(), "p")
	if err != nil {
		t.Fatal(err)
	}
	desc := map[string]string{}
	for _, s := range plan.Chain.Steps {
		desc[s.ID] = s.Description
	}
	if got := desc["get_product_before"]; !strings.Contains(got, "the read ahead of a stock change") {
		t.Fatalf("get_product_before description = %q, want the alias note", got)
	}
	if got := desc["get_product_after"]; got != "Reads one product." {
		t.Fatalf("an alias with no note keeps the rpc summary, got %q", got)
	}
}
