package contract_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

const (
	listWithoutNeedsOverlay = `apiVersion: shrt/contract/v1
domain: catalog
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        required: [sku]
        fields:
            sku:
                value: sku-${vars.tag}
        status: draft
` + listProductsEntry
	listOnlyOverlay   = "apiVersion: shrt/contract/v1\ndomain: catalog\nrpcs:\n" + listProductsEntry
	listProductsEntry = `    shop.catalog.v1.ProductService/ListProducts:
        summary: lists products whose sku starts with a prefix
        required: [NONE]
        fields:
            sku_prefix:
                value: sku-
        status: draft
`
)

func TestPlanForAListAddsTheWriteThatCreatesWhatItLists(t *testing.T) {
	p, err := contract.BuildPlan("shop.catalog.v1.ProductService/ListProducts", libraryFrom(t, listWithoutNeedsOverlay), catalogtest.Shop(), "list")
	if err != nil {
		t.Fatal(err)
	}
	if stepIndex(p.Chain, "create_product") < 0 || stepIndex(p.Chain, "create_product") > stepIndex(p.Chain, "list_products") {
		t.Fatalf("a list of nothing asserts nothing: the plan must create a product before listing: %v", p.Order)
	}
	notes := strings.Join(p.Notes, "\n")
	if !strings.Contains(notes, "declares no needs:") || !strings.Contains(notes, "needs: [shop.catalog.v1.ProductService/CreateProduct]") {
		t.Fatalf("the plan says it inferred the producer and how to declare it: %v", p.Notes)
	}
}

func TestPlanForAListWithNoKnownCreatorSaysTheListIsEmpty(t *testing.T) {
	p, err := contract.BuildPlan("shop.catalog.v1.ProductService/ListProducts", libraryFrom(t, listOnlyOverlay), catalogtest.Shop(), "list")
	if err != nil {
		t.Fatal(err)
	}
	if stepIndex(p.Chain, "create_product") >= 0 {
		t.Fatalf("no contract for the creator: nothing is inferred")
	}
	if !strings.Contains(strings.Join(p.Notes, "\n"), "nothing in this chain creates") {
		t.Fatalf("the plan must say the list will be empty: %v", p.Notes)
	}
}

func TestContractLintWarnsOfAListWithoutNeeds(t *testing.T) {
	issues := contract.LintLibrary(libraryFrom(t, listWithoutNeedsOverlay), catalogtest.Shop())
	if !slices.ContainsFunc(issues, func(i contract.Issue) bool {
		return strings.HasSuffix(i.RPC, "/ListProducts") && i.Field == "needs" && strings.Contains(i.Message, "CreateProduct")
	}) {
		t.Fatalf("contract lint must warn that ListProducts lists Product and declares no needs: %v", issues)
	}
}
