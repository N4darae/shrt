package contract_test

import (
	"strings"
	"testing"
)

func TestPlanGivesABatchLineTheLargeValueItsSingleItemRpcGets(t *testing.T) {
	p := editedPlan(t, func(_, body string) string {
		return strings.Replace(body, "                note: one AddStock per line, applied independently in order\n", "                note: at least one line\n", 1)
	}, "AddStockBatch")
	raw, _ := p.YAML()
	text := string(raw)
	large := planStep(t, p, "add_stock_batch_qty_large")
	if bodyAt(t, large, "lines.0.qty") != "12345" || bodyAt(t, large, "lines.1.qty") == "12345" {
		t.Fatalf("one line carries the large quantity AddStock is probed with, beside a normal line:\n%s", text)
	}
	wantExpect(t, large, "results.0.status.code", "SUCCESS")
	wantExpect(t, large, "results.1.status.code", "SUCCESS")
	gte := false
	for _, e := range large.Expect {
		gte = gte || (e.Path == "results.0.qty_on_hand" && e.Gte == "12345")
	}
	if !gte {
		t.Fatalf("the large line's reported level is at least what it added, so a cap on it fails:\n%s", text)
	}
	wantExpect(t, planStep(t, p, "get_product_after_add_stock_batch_qty_large"), "product.qty_on_hand", "${add_stock_batch_qty_large.results.0.qty_on_hand}")
}
