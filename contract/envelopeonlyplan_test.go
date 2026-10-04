package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

const factsOverlay = `apiVersion: shrt/contract/v1
domain: catalog
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: Adds a product.
        required: [sku]
        fields:
            sku:
                value: SKU-1
        exports:
            product.id_product: the id stock refers to
        status: draft
    shop.catalog.v1.StockService/AddStock:
        summary: Adds stock.
        required: [id_product]
        fields:
            id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
            qty:
                value: "5"
        terminal:
            qty_on_hand: the level after the add
        status: draft
`

func envelopeOnlyIssues(issues []chain.Issue) map[string]chain.Issue {
	out := map[string]chain.Issue{}
	for _, i := range issues {
		if i.Kind == chain.KindEnvelopeOnly {
			out[i.Step] = i
		}
	}
	return out
}

func TestAPlannedStepAssertingOnlyTheVerdictFailsStrictLint(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	cat := catalogtest.Shop()
	lib := shopLibrary(t, factsOverlay)
	plan, err := contract.BuildPlan(shopAddStock, lib, cat, "p")
	if err != nil {
		t.Fatal(err)
	}
	planned := envelopeOnlyIssues(contract.LintChain(plan.Chain, cat, contract.ChainLintOptions{Library: lib, Strict: true}))
	if len(planned) > 0 {
		t.Fatalf("the plan asserts what AddStock declares under terminal, so strict lint passes it: %+v", planned)
	}
	stock, _ := plan.Chain.Step("add_stock")
	kept := []chain.Expectation{}
	for _, e := range stock.Expect {
		if e.Path == "status.code" {
			kept = append(kept, e)
		}
	}
	stock.Expect = kept
	loose := envelopeOnlyIssues(contract.LintChain(plan.Chain, cat, contract.ChainLintOptions{Library: lib}))
	strict := envelopeOnlyIssues(contract.LintChain(plan.Chain, cat, contract.ChainLintOptions{Library: lib, Strict: true}))
	if _, flagged := loose["create_product"]; flagged {
		t.Fatalf("create_product now asserts the price it sent is the price stored, so it is not envelope-only: %+v", loose)
	}
	for _, id := range []string{"add_stock"} {
		if _, ok := loose[id]; !ok || loose[id].IsError() {
			t.Fatalf("step %s asserts only status.code and its contract declares facts: want a warning, got %+v", id, loose)
		}
		if !strict[id].IsError() {
			t.Fatalf("-strict must fail step %s, got %+v", id, strict[id])
		}
	}
	if !strings.Contains(strict["add_stock"].Message, "qty_on_hand") {
		t.Fatalf("the issue must name the declared fact to assert: %s", strict["add_stock"].Message)
	}

	stock.Expect = append(stock.Expect, chain.Expectation{Path: "qty_on_hand", Equals: 5})
	after := envelopeOnlyIssues(contract.LintChain(plan.Chain, cat, contract.ChainLintOptions{Library: lib, Strict: true}))
	if _, still := after["add_stock"]; still {
		t.Fatalf("a data assertion clears the issue, got %+v", after["add_stock"])
	}
}

func TestARefusalProbeIsNotEnvelopeOnly(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	cat := catalogtest.Shop()
	lib := shopLibrary(t, factsOverlay)
	c := &chain.Chain{Name: "probe", Steps: []*chain.Step{{
		ID: "refused", Call: shopAddStock, Body: map[string]any{"id_product": "p", "qty": "0"},
		Expect: []chain.Expectation{{Path: "status.code", Equals: "REJECTED"}},
	}}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if got := envelopeOnlyIssues(contract.LintChain(c, cat, contract.ChainLintOptions{Library: lib, Strict: true})); len(got) != 0 {
		t.Fatalf("a refusal assertion is the data of a probe, got %+v", got)
	}
}
