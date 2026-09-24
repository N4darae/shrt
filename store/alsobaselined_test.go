package store_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestProposalSummaryShowsUnassertedValuesThatBecomeTheBaseline(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	rec := &runner.Record{
		RunID: "run-m", Chain: "multi", Target: "http://localhost", Status: runner.StatusPassed,
		Vars:     map[string]any{"tag": "m2x"},
		Volatile: []string{"**.created_at"},
		Steps: []*runner.StepRecord{{
			Index: 1, ID: "create_order", Call: "OrderService/CreateOrder", Status: runner.StatusPassed,
			Request: json.RawMessage(`{"id_customer":"cus-1"}`),
			Response: json.RawMessage(`{"status":{"code":"SUCCESS","details":[],"message":""},"order":{
				"id_order":"ord-8f2a1c","id_customer":"cus-1","status":"ORDER_STATUS_PENDING","total_minor":"300",
				"created_at":"2026-09-24T19:14:11Z","updated_at":"2026-09-24T19:14:11Z","note":"for m2x",
				"lines":[{"id_product":"prd-1","qty":"1","unit_price_minor":"100"},{"id_product":"prd-2","qty":"1","unit_price_minor":"200"}]}}`),
			Expect: []chain.ExpectResult{
				{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
				{Path: "order.status", Rule: "equals", Want: "ORDER_STATUS_PENDING", Got: "ORDER_STATUS_PENDING", Passed: true},
			},
		}},
	}
	text := store.ProposalSummary(&store.Proposal{Chain: rec.Chain, RunID: rec.RunID}, rec)
	row := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "| 1 |") {
			row = line
		}
	}
	if !strings.Contains(row, "also baselined: order.total_minor=300") {
		t.Fatalf("an unasserted business value that becomes the baseline must be shown to the approver:\n%s", row)
	}
	for _, hidden := range []string{"id_order", "created_at", "updated_at", "for m2x", "also baselined: order.status", "status.message"} {
		if strings.Contains(row[strings.Index(row, "also baselined"):], hidden) {
			t.Errorf("ids, timestamps, volatile values, fixture echoes and asserted paths stay out of the list (%s):\n%s", hidden, row)
		}
	}
	if !strings.Contains(row, "more") {
		t.Errorf("a long list is capped with +N more:\n%s", row)
	}
}
