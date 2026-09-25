package contract_test

import (
	"strings"
	"testing"
)

func TestABatchIsProbedWithTheRefusedLineFirstLastAndNamingAnUnknownID(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "AddStockBatch")
	first := planStep(t, p, "add_stock_batch_partial_first")
	if bodyAt(t, first, "lines.0.qty") != "0" || bodyAt(t, first, "lines.1.qty") == "0" {
		t.Fatalf("the refused line comes first:\n%s", text)
	}
	wantExpect(t, first, "results.1.status.code", "SUCCESS")
	wantExists(t, first, "results.2", false)
	last := planStep(t, p, "add_stock_batch_partial_last")
	if bodyAt(t, last, "lines.1.qty") != "0" || bodyAt(t, last, "lines.0.qty") == "0" {
		t.Fatalf("the refused line comes last:\n%s", text)
	}
	wantExpect(t, last, "results.0.status.code", "SUCCESS")
	unknown := planStep(t, p, "add_stock_batch_unknown_id_product_line")
	if got := bodyAt(t, unknown, "lines.1.id_product"); !strings.HasSuffix(got, "}-unknown") {
		t.Fatalf("the last line names an id no record has, got %s:\n%s", got, text)
	}
	wantExpect(t, unknown, "results.1.status.details.0.reason", "ProductNotFound")
	wantExpect(t, unknown, "results.0.status.code", "SUCCESS")
	before := planStep(t, p, "get_product_2_before_add_stock_batch_unknown_id_product_line")
	after := planStep(t, p, "get_product_2_after_add_stock_batch_unknown_id_product_line")
	wantExpect(t, after, "product.qty_on_hand", "${"+before.ID+".product.qty_on_hand}")
	wantExpect(t, planStep(t, p, "get_product_after_add_stock_batch_unknown_id_product_line"), "product.qty_on_hand",
		"${add_stock_batch_unknown_id_product_line.results.0.qty_on_hand}")
	wantExpect(t, planStep(t, p, "get_product_after_add_stock_batch_partial_first"), "product.qty_on_hand",
		"${get_product_before_add_stock_batch_partial_first.product.qty_on_hand}")
	if !strings.Contains(notes, "also refuse one line each") {
		t.Fatalf("the plan says what the positional probes prove:\n%s", notes)
	}
}
