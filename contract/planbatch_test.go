package contract_test

import (
	"strings"
	"testing"
)

func TestPlanForABatchWithPerItemFailuresRefusesAMiddleLineAndChecksTheLinesAroundIt(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "AddStockBatch")
	partial := planStep(t, p, "add_stock_batch_partial")
	if got := bodyAt(t, partial, "lines.1.qty"); got != "0" {
		t.Fatalf("the middle line carries a qty the contract refuses (zero or negative), got %s:\n%s", got, text)
	}
	if bodyAt(t, partial, "lines.2.id_product") != bodyAt(t, partial, "lines.1.id_product") {
		t.Fatalf("the line after the refused one targets the same product, so its reported stock shows whether the refused line leaked:\n%s", text)
	}
	if got := bodyAt(t, partial, "lines.2.qty"); got == "0" {
		t.Fatalf("the last line is valid:\n%s", text)
	}
	wantExpect(t, partial, "status.code", "SUCCESS")
	wantExpect(t, partial, "results.0.status.code", "SUCCESS")
	wantExpect(t, partial, "results.1.status.details.0.app_code", 1203)
	wantExpect(t, partial, "results.1.status.details.0.reason", "InvalidQty")
	wantExpect(t, partial, "results.2.status.code", "SUCCESS")
	wantExists(t, partial, "results.3", false)
	refused := false
	for _, e := range partial.Expect {
		if e.Path == "results.1.status.code" && e.NotEqual != nil {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("the middle line's own verdict is not the ok value:\n%s", text)
	}

	read := planStep(t, p, "get_product_after_add_stock_batch_partial")
	wantExpect(t, read, "product.qty_on_hand", int64(6))
	for _, e := range read.Expect {
		if s, _ := e.Equals.(string); strings.Contains(s, "${add_stock_batch_partial.") {
			t.Fatalf("the read-back is judged apart from the batch's answer, got %s equals %s:\n%s", e.Path, s, text)
		}
	}
	if !strings.Contains(read.Description, "the level the plan works out") {
		t.Fatalf("the read-back says it asserts the worked-out level, got %q", read.Description)
	}
	wantExpect(t, planStep(t, p, "get_product_2_after_add_stock_batch_partial"), "product.qty_on_hand", int64(8))
	if !strings.Contains(notes, "add_stock_batch_partial") {
		t.Fatalf("the plan says what the partial batch proves: %s", notes)
	}
}
