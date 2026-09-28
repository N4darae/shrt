package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

const (
	listSpotCreateW = `{"product":{"id_product":"prd-f9ef06e736d5","sku":"sku-w","price_minor":"1250"}}`
	listSpotCreateG = `{"product":{"id_product":"prd-bee2dc1a437c","sku":"sku-g","price_minor":"799"}}`
	listSpotList    = `{"products":[{"id_product":"prd-bee2dc1a437c","sku":"sku-g","price_minor":"799"},{"id_product":"prd-f9ef06e736d5","sku":"sku-w","price_minor":"1250"}]}`
	listRunCreateW  = `{"product":{"id_product":"prd-0066f622803d","sku":"sku-w","price_minor":"1250"}}`
	listRunCreateG  = `{"product":{"id_product":"prd-1bcacf4cff60","sku":"sku-g","price_minor":"799"}}`
	listRunReversed = `{"products":[{"id_product":"prd-0066f622803d","sku":"sku-w","price_minor":"1250"},{"id_product":"prd-1bcacf4cff60","sku":"sku-g","price_minor":"799"}]}`
)

func listSpot() *store.SafeSpot {
	return &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create_w", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(listSpotCreateW)},
		{ID: "create_g", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(listSpotCreateG)},
		{ID: "list", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(listSpotList)},
	}}
}

func listRun(list string, unordered []string) *runner.Record {
	rec := runOf("run",
		stepAs("create_w", runner.StatusPassed, listRunCreateW),
		stepAs("create_g", runner.StatusPassed, listRunCreateG),
		stepAs("list", runner.StatusPassed, list))
	rec.Steps[2].Unordered = unordered
	return rec
}

func TestAnUnorderedListIsComparedAsAMultisetWithIdsPairedByContent(t *testing.T) {
	rep := diff.Compare(listSpot(), listRun(listRunReversed, []string{"products"}))
	if !rep.Clean() {
		t.Fatalf("a list declared unordered holding the same items in another order is no drift:\n%s", rep.Text())
	}
	changed := `{"products":[{"id_product":"prd-0066f622803d","sku":"sku-w","price_minor":"1250"},{"id_product":"prd-1bcacf4cff60","sku":"sku-g","price_minor":"800"}]}`
	rep = diff.Compare(listSpot(), listRun(changed, []string{"products"}))
	if rep.Clean() || !strings.Contains(rep.Text(), "products.0.price_minor want=799 got=800") {
		t.Fatalf("a changed item of an unordered list is still drift, reported at the safe spot's position:\n%s", rep.Text())
	}
}

func TestAReorderedListWithoutTheDeclarationIsNamedAndPointsAtIt(t *testing.T) {
	rep := diff.Compare(listSpot(), listRun(listRunReversed, nil))
	if rep.Clean() {
		t.Fatal("without the declaration the order is compared, so another order is drift")
	}
	text := rep.Text()
	if !strings.Contains(text, "list products") || !strings.Contains(text, "unordered: [products]") {
		t.Fatalf("verify must say the list holds the same items in another order and name the declaration:\n%s", text)
	}
	if !rep.OnlyReordered() {
		t.Fatalf("every change here is the reordered list:\n%s", text)
	}
	changed := `{"products":[{"id_product":"prd-0066f622803d","sku":"sku-w","price_minor":"1250"},{"id_product":"prd-1bcacf4cff60","sku":"sku-g","price_minor":"800"}]}`
	rep = diff.Compare(listSpot(), listRun(changed, nil))
	if strings.Contains(rep.Text(), "unordered: [products]") || rep.OnlyReordered() {
		t.Fatalf("a list whose items changed is not the same set in another order:\n%s", rep.Text())
	}
}

func TestAReorderAnExpectationReadByPositionIsNotOfferedUnordered(t *testing.T) {
	rec := listRun(listRunReversed, nil)
	rec.Steps[2].Status = runner.StatusFailed
	rec.Steps[2].Expect = []chain.ExpectResult{{Path: "products.0.sku", Rule: "equals", Want: "sku-g", Got: "sku-w"}}
	text := diff.Compare(listSpot(), rec).Text()
	if strings.Contains(text, "unordered: [") || !strings.Contains(text, "which passed there, failed: products.0.sku") {
		t.Fatalf("an order the chain relied on and the safe spot held is a regression, not a list to declare unordered:\n%s", text)
	}
}
