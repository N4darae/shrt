package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func TestChainNewNotesWhatContractPlanNotesAboutTheBody(t *testing.T) {
	overlay := strings.Replace(shopCatalogOverlay, "required: [sku]", "required: [sku, name]", 1)
	lib := shopLibrary(t, overlay)
	_, notes, err := contract.ScaffoldChain("cn", "", []string{shopCreateProduct}, []string{"create_product"}, lib, catalogtest.Shop())
	if err != nil {
		t.Fatal(err)
	}
	if !anyNote(notes, "name is required and has no usable value") {
		t.Fatalf("chain new leaves name empty and must say so, as contract plan does: %v", notes)
	}
	if !anyNote(notes, "price_minor still carries the scaffold's numeric zero") {
		t.Fatalf("chain new leaves price_minor at 0 and must say so, as contract plan does: %v", notes)
	}
}

func TestChainNewPrintsEachKindOfNoteOnceNamingItsSteps(t *testing.T) {
	overlay := strings.Replace(shopCatalogOverlay, "required: [sku]", "required: [TODO]", 1)
	lib := shopLibrary(t, overlay)
	_, notes, err := contract.ScaffoldChain("cn", "", []string{shopCreateProduct, shopCreateProduct, shopCreateProduct},
		[]string{"create_product", "create_product_2", "create_product_3"}, lib, catalogtest.Shop())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(notes, "\n")
	if n := strings.Count(joined, "required is an unfilled TODO"); n != 1 {
		t.Fatalf("the unfilled required note should be printed once, got %d:\n%s", n, joined)
	}
	if !strings.Contains(joined, "steps create_product, create_product_2, create_product_3: required is an unfilled TODO") {
		t.Fatalf("the grouped note should name every step it applies to:\n%s", joined)
	}
}
