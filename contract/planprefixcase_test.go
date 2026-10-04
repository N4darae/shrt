package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func planPrefixNote(t *testing.T, note string) (*contract.Plan, string) {
	return shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.catalog.v1.ProductService/CreateProduct"]; c != nil {
			c.Fields["sku"].Value = "sku-${vars.tag}-a"
			c.Fields["sku"].Note = "unique"
		}
		if c := rpcs["shop.catalog.v1.ProductService/ListProducts"]; c != nil {
			c.Fields["sku_prefix"].Value = ""
			c.Fields["sku_prefix"].SameAs = "shop.catalog.v1.ProductService/CreateProduct->sku"
			c.Fields["sku_prefix"].Note = note
		}
	}, "ListProducts")
}

func findStep(p *contract.Plan, id string) *chain.Step {
	for _, s := range p.Chain.Steps {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func TestAPrefixWhoseCaseRuleIsUnstatedIsListedInAnotherCaseAssertingNoMembership(t *testing.T) {
	p, text := planPrefixNote(t, "a prefix filter; empty lists all")
	if findStep(p, "create_product_prefix_case") != nil {
		t.Fatalf("no case rule is stated, so no fixture may be asserted absent for its case\n%s", text)
	}
	probe := planStep(t, p, "list_products_prefix_case")
	prefix := bodyAt(t, planStep(t, p, "list_products"), "sku_prefix")
	got := bodyAt(t, probe, "sku_prefix")
	if got == prefix || !strings.EqualFold(got, prefix) {
		t.Fatalf("the probe lists with the target's prefix in another letter case: %q vs %q\n%s", got, prefix, text)
	}
	for _, e := range probe.Expect {
		if e.Path != "status.code" && !(e.Path == "products.4" && e.Exists != nil && !*e.Exists) {
			t.Fatalf("the probe asserts only the verdict and the prefix's upper bound, not which items it lists: %+v\n%s", e, text)
		}
	}
	cat, lib := shopDemo(t)
	for _, i := range strictLint(t, "case.yaml", text, cat, lib, chain.LintOptions{}) {
		if i.Step == probe.ID && (i.IsError() || i.Kind == chain.KindEnvelopeOnly) {
			t.Fatalf("the case probe fails strict lint: [%s] %s\n%s", i.Kind, i.Message, text)
		}
	}
}

func TestAPrefixStatedCaseSensitiveKeepsTheAssertedCaseFixture(t *testing.T) {
	p, text := planPrefixNote(t, "a prefix filter, case-sensitive; empty lists all")
	planStep(t, p, "create_product_prefix_case")
	if findStep(p, "list_products_prefix_case") != nil {
		t.Fatalf("a stated case rule is asserted by the fixture, not left to the safe spot\n%s", text)
	}
}
