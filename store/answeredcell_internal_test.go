package store

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestTheAnsweredCellShowsEveryAssertedPairInFullNeverCutsAPath(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	t.Cleanup(func() { chain.SetEnvelope("", "") })
	st := &runner.StepRecord{ID: "add_stock_batch", Status: runner.StatusPassed,
		Response: []byte(`{"status":{"code":"SUCCESS"},"results":[{"status":{"code":"SUCCESS"},"qtyOnHand":8},` +
			`{"status":{"code":"REJECTED","details":[{"reason":"InvalidQty","appCode":1203}]},"qtyOnHand":0}]}`),
		Expect: []chain.ExpectResult{
			{Path: "status.code", Rule: "equals", Got: "SUCCESS", Passed: true},
			{Path: "results.0.status.code", Rule: "equals", Got: "SUCCESS", Passed: true},
			{Path: "results.0.qty_on_hand", Rule: "equals", Got: 8, Passed: true},
			{Path: "results.1.status.code", Rule: "equals", Got: "REJECTED", Passed: true},
			{Path: "results.1.status.details.0.reason", Rule: "equals", Got: "InvalidQty", Passed: true},
			{Path: "results.1.status.details.0.app_code", Rule: "equals", Got: 1203, Passed: true},
		}}
	cell := answeredSummary(st)
	if strings.Contains(cell, "…") {
		t.Fatalf("the answered cell must not cut inside a path or value: %q", cell)
	}
	for _, want := range []string{"results.0.qty_on_hand=8", "results.1.status.details.0.reason=InvalidQty", "results.1.status.details.0.app_code=1203"} {
		if !strings.Contains(cell, want) {
			t.Fatalf("every asserted value is what the approver approves, so %s is shown: %q", want, cell)
		}
	}
	for _, part := range strings.Fields(cell) {
		if strings.HasPrefix(part, "results.") && !strings.Contains(part, "=") {
			t.Fatalf("a path=value pair was cut: %q in %q", part, cell)
		}
	}
}
