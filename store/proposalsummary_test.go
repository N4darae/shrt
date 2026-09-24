package store_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func batchRun() *runner.Record {
	return &runner.Record{
		RunID: "run-b", Chain: "batch", Target: "http://localhost", Status: runner.StatusPassed,
		Steps: []*runner.StepRecord{{
			Index: 1, ID: "batch_one_bad_line", Call: "StockService/AddStockBatch", Status: runner.StatusPassed,
			Request: json.RawMessage(`{"lines":[{"id_product":"prd-36a53e2c593c","qty":6},{"id_product":"prd-36a53e2c593c","qty":0}]}`),
			Response: json.RawMessage(`{"status":{"code":"SUCCESS","details":[],"message":""},"results":[
				{"status":{"code":"SUCCESS","details":[]}},
				{"status":{"code":"REJECTED","details":[{"app_code":1203,"reason":"InvalidQty"}],"message":"qty must be greater than zero"}},
				{"status":{"code":"REJECTED","details":[{"app_code":1203,"reason":"InvalidQty"}]}}]}`),
			Expect: []chain.ExpectResult{
				{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
				{Path: "results.0.note", Rule: "equals", Want: strings.Repeat("x", 200), Got: strings.Repeat("x", 200), Passed: true},
				{Path: "results.1.status.code", Rule: "equals", Want: "REJECTED", Got: "REJECTED", Passed: true},
				{Path: "results.1.status.details.0.app_code", Rule: "equals", Want: 1203, Got: 1203, Passed: true},
			},
		}},
	}
}

func TestProposalSummaryShowsPerItemOutcomeSentAndReadableAssertions(t *testing.T) {
	defer chain.SetEnvelope("", "")
	defer chain.SetItemEnvelope("")
	chain.SetEnvelope("status.code", "SUCCESS")
	chain.SetItemEnvelope("results[].status.code")
	rec := batchRun()
	text := store.ProposalSummary(&store.Proposal{Chain: rec.Chain, RunID: rec.RunID}, rec)
	if !strings.Contains(text, "| # | step | sent | asserted, all held | backend answered |") {
		t.Fatalf("the table must carry a sent column:\n%s", text)
	}
	if !strings.Contains(text, "SUCCESS; items: 1 SUCCESS, 2 REJECTED 1203 InvalidQty;") {
		t.Fatalf("a batch whose items were refused must say so, not only SUCCESS:\n%s", text)
	}
	if !strings.Contains(text, "| lines.0.qty=6 lines.1.qty=0 lines.0.id_product=prd-36a53e2c593c") {
		t.Fatalf("the sent column must excerpt the request:\n%s", text)
	}
	if !strings.Contains(text, "results.1.status.details.0.app_code equals 1203") {
		t.Fatalf("a long value must be abbreviated without cutting later assertions:\n%s", text)
	}
	if strings.Contains(text, strings.Repeat("x", 30)) {
		t.Fatalf("a long asserted value must be abbreviated:\n%s", text)
	}
	if rows := strings.Count(text, "\n| 1 |"); rows != 1 {
		t.Fatalf("the table must keep one row per step, got %d:\n%s", rows, text)
	}
}
