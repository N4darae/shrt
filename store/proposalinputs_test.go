package store_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestProposalSummaryLeadsWithTheInputsAndTheAssertedAnswer(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	rec := &runner.Record{
		RunID: "run-o", Chain: "orders", Target: "http://localhost", Status: runner.StatusPassed,
		Steps: []*runner.StepRecord{{
			Index: 6, ID: "create_order", Call: "OrderService/CreateOrder", Status: runner.StatusPassed,
			Request: json.RawMessage(`{"id_customer":"cus-8f2c61d0a1b2","idempotency_key":"0b8e2f4c-6a51-4c1e-9d3e-7f0a2b4c6d8e",` +
				`"lines":[{"id_product":"prd-e7a980d9914a","qty":3},{"id_product":"prd-1c2d3e4f5a6b","qty":2}]}`),
			Response: json.RawMessage(`{"status":{"code":"SUCCESS","details":[],"message":""},"order":{"id_order":"ord-0a1b2c3d4e5f",` +
				`"status":"ORDER_STATUS_PENDING","total_minor":4548,"id_customer":"cus-8f2c61d0a1b2"}}`),
			Expect: []chain.ExpectResult{
				{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
				{Path: "order.status", Rule: "equals", Want: "ORDER_STATUS_PENDING", Got: "ORDER_STATUS_PENDING", Passed: true},
				{Path: "order.total_minor", Rule: "equals", Want: 4548, Got: 4548, Passed: true},
				{Path: "order.id_customer", Rule: "equals", Want: "cus-8f2c61d0a1b2", Got: "cus-8f2c61d0a1b2", Passed: true},
			},
		}},
	}
	text := store.ProposalSummary(&store.Proposal{Chain: rec.Chain, RunID: rec.RunID}, rec)
	row := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "| 6 |") {
			row = line
		}
	}
	cells := strings.Split(row, " | ")
	if len(cells) < 5 {
		t.Fatalf("no row for the step:\n%s", text)
	}
	sent, answered := cells[2], cells[4]
	if !strings.HasPrefix(sent, "lines.0.qty=3 lines.1.qty=2") {
		t.Fatalf("the sent column must lead with the literal inputs a person judges, the qtys, not ids: %q", sent)
	}
	if !strings.Contains(answered, "order.total_minor=4548") || !strings.Contains(answered, "order.status=ORDER_STATUS_PENDING") {
		t.Fatalf("backend answered must show the values the step asserts, not only the envelope: %q", answered)
	}
	if len([]rune(row)) > 700 {
		t.Fatalf("the row must stay readable in chat: %d runes\n%s", len([]rune(row)), row)
	}
}
