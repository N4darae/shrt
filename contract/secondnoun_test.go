package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func TestTheSecondItemNoteNamesWhatSendsIt(t *testing.T) {
	lib := shopLibrary(t, pricedProducerOverlay)
	refs := []string{shopCreateProduct, "shop.customers.v1.CustomerService/CreateCustomer", shopCreateOrder}
	ids := []string{"create_product", "create_customer", "create_order"}
	_, notes, err := contract.ScaffoldChain("cn", "", refs, ids, lib, catalogtest.Shop())
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(notes, "\n")
	if !strings.Contains(all, "lines is repeated, so the chain sends two items") || strings.Contains(all, "the plan") {
		t.Fatalf("chain new writes a chain, not a plan: %v", notes)
	}
	plan, err := contract.BuildPlan(shopCreateOrder, lib, catalogtest.Shop(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if !anyNote(plan.Notes, "lines is repeated, so the plan sends two items") {
		t.Fatalf("contract plan names the plan: %v", plan.Notes)
	}
	m, err := catalogtest.Shop().Lookup(shopCreateOrder)
	if err != nil {
		t.Fatal(err)
	}
	if _, notes := contract.ForCuratedWithNotes(m, lib, catalogtest.Shop()); !anyNote(notes, "lines is repeated, so the step sends two items") {
		t.Fatalf("a scaffolded step names the step: %v", notes)
	}
}
