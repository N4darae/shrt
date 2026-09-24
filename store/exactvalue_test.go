package store_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestProposalSummaryShowsABlankValueExactly(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	rec := &runner.Record{
		RunID: "run-s", Chain: "blank", Target: "http://localhost", Status: runner.StatusPassed,
		Steps: []*runner.StepRecord{{
			Index: 1, ID: "create_blank_sku", Call: "ProductService/CreateProduct", Status: runner.StatusPassed,
			Request:  json.RawMessage(`{"sku":"   ","name":"a  b"}`),
			Response: json.RawMessage(`{"status":{"code":"REJECTED","message":"sku   is blank"}}`),
			Expect: []chain.ExpectResult{
				{Path: "status.code", Rule: "equals", Want: "REJECTED", Got: "REJECTED", Passed: true},
			},
		}},
	}
	text := store.ProposalSummary(&store.Proposal{Chain: rec.Chain, RunID: rec.RunID}, rec)
	if !strings.Contains(text, `sku="   "`) {
		t.Errorf("a whitespace-only value must be shown exactly, quoted:\n%s", text)
	}
	if !strings.Contains(text, "REJECTED sku is blank") {
		t.Errorf("free text outside a quoted value is still flattened:\n%s", text)
	}
}
