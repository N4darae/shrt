package store_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestProposalBriefSummarisesARunInsteadOfTablingEveryStep(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	rec := &runner.Record{RunID: "run-1", Chain: "orders", Target: "http://localhost", Status: runner.StatusPassed}
	for i := 1; i <= 40; i++ {
		rec.Steps = append(rec.Steps, &runner.StepRecord{
			Index: i, ID: fmt.Sprintf("create_order_%d", i), Call: "shop.orders.v1.OrderService/CreateOrder", Status: runner.StatusPassed,
			Request:  json.RawMessage(`{"qty":2}`),
			Response: json.RawMessage(`{"status":{"code":"SUCCESS"},"order":{"total_minor":500}}`),
			Expect: []chain.ExpectResult{
				{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
				{Path: "order.total_minor", Rule: "equals", Want: 500, Got: 500, Passed: true},
			},
		})
	}
	rec.Steps = append(rec.Steps,
		&runner.StepRecord{Index: 41, ID: "create_order_unknown", Call: "shop.orders.v1.OrderService/CreateOrder", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"status":{"code":"REJECTED","details":[{"app_code":1302,"reason":"OrderNotFound"}]}}`),
			Expect:   []chain.ExpectResult{{Path: "status.code", Rule: "not_equal", Want: "SUCCESS", Got: "REJECTED", Passed: true}}},
		&runner.StepRecord{Index: 42, ID: "fetch_order", Call: "shop.orders.v1.OrderService/FetchOrder", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"status":{"code":"SUCCESS"}}`)},
		&runner.StepRecord{Index: 43, ID: "fetch_again", Call: "shop.orders.v1.OrderService/FetchOrder", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"status":{"code":"SUCCESS"}}`), Warning: "refused in-band",
			Expect: []chain.ExpectResult{{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true}}},
	)
	p := &store.Proposal{Chain: rec.Chain, RunID: rec.RunID, Target: rec.Target, Checked: "totals are 5 x 100",
		Unstable: []string{"fetch_order order.note: a -> b"}, ComparedTo: "run-0"}
	brief := store.ProposalBrief(p, rec, "reach CreateOrder")
	full := store.ProposalSummary(p, rec)
	if len(brief) > len(full)/3 {
		t.Fatalf("the brief is a summary, %d bytes against the table's %d:\n%s", len(brief), len(full), brief)
	}
	for _, want := range []string{
		"43/43 steps passed", "What it does: reach CreateOrder", "totals are 5 x 100",
		"43 steps calling CreateOrder ×41, FetchOrder ×2",
		"SUCCESS ×42, REJECTED 1302 OrderNotFound ×1",
		"82 assertions, all held", "order.total_minor ×40",
		"1 step(s) assert nothing", "`fetch_order`",
		"assert only the verdict: `fetch_again`",
		"1 step(s) carry a warning", "refused in-band",
		"1 field(s) differ from the earlier passing run", "fetch_order order.note",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lacks %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "| # | step |") || strings.Contains(brief, "create_order_17") {
		t.Fatalf("the brief does not table every step:\n%s", brief)
	}
}

func TestAProposalRowNamesWhatToCheckInOneLine(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	rec := &runner.Record{RunID: "run-1", Chain: "orders", Status: runner.StatusPassed, Volatile: []string{"**.created_at"}, Steps: []*runner.StepRecord{
		{ID: "create", Call: "x.v1.OrderService/CreateOrder", Status: runner.StatusPassed, Response: json.RawMessage(`{"status":{"code":"SUCCESS"}}`),
			Expect: []chain.ExpectResult{{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true}}},
		{ID: "unknown", Call: "x.v1.OrderService/FetchOrder", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"status":{"code":"REJECTED","details":[{"app_code":1302,"reason":"OrderNotFound"}]}}`),
			Expect:   []chain.ExpectResult{{Path: "status.code", Rule: "equals", Want: "REJECTED", Got: "REJECTED", Passed: true}}},
		{ID: "fetch", Call: "x.v1.OrderService/FetchOrder", Status: runner.StatusPassed, Response: json.RawMessage(`{"status":{"code":"SUCCESS"}}`)},
	}}
	row := store.ProposalRowOf(&store.Proposal{Chain: "orders", RunID: "run-1", ComparedTo: "run-0"}, rec)
	if row.Steps != "3/3" || row.Refusals != "REJECTED 1302 OrderNotFound ×1" ||
		row.Check != "1 step(s) assert nothing; 1 step(s) assert only the verdict" || row.Volatile != "`**.created_at`" {
		t.Errorf("one row per chain, with what to look at: %+v", row)
	}
}

func TestAProposalRowReadsAStreamingStepsEnvelopeInItsMessages(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	watch := func(id, code string, expect ...chain.ExpectResult) *runner.StepRecord {
		return &runner.StepRecord{ID: id, Call: "x.v1.OrderService/WatchOrder", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"messages":[{"status":{"code":"` + code + `"},"order":{"id":"o1"}}]}`), Expect: expect}
	}
	ok := chain.ExpectResult{Path: "messages.0.status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true}
	rec := &runner.Record{RunID: "run-1", Chain: "watch", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		watch("watch", "SUCCESS", ok),
		watch("watch_record", "SUCCESS", ok, chain.ExpectResult{Path: "messages.0.order.id", Rule: "equals", Want: "o1", Got: "o1", Passed: true}),
		watch("watch_unknown", "REJECTED", chain.ExpectResult{Path: "messages.0.status.code", Rule: "not_equal", Want: "SUCCESS", Got: "REJECTED", Passed: true}),
	}}
	row := store.ProposalRowOf(&store.Proposal{Chain: "watch", RunID: "run-1", ComparedTo: "run-0"}, rec)
	if row.Refusals != "REJECTED ×1" || row.Check != "1 step(s) assert only the verdict" {
		t.Errorf("messages.N.status.code is the envelope of a streamed step: %+v", row)
	}
	brief := store.ProposalBrief(&store.Proposal{Chain: "watch", RunID: "run-1", ComparedTo: "run-0"}, rec, "")
	if strings.Contains(brief, "nothing at") || !strings.Contains(brief, "assert only the verdict: `watch`") {
		t.Errorf("the brief reads the streamed envelope too:\n%s", brief)
	}
}

func TestAProposalRowCountsARefusedBatchLineItAsserts(t *testing.T) {
	defer chain.SetEnvelope("", "")
	defer chain.SetItemEnvelope("")
	chain.SetEnvelope("status.code", "SUCCESS")
	chain.SetItemEnvelope("results[].status.code")
	rec := &runner.Record{RunID: "run-1", Chain: "batch", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "batch", Call: "x.v1.StockService/AddStockBatch", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"status":{"code":"SUCCESS"},"results":[{"status":{"code":"SUCCESS"}},{"status":{"code":"REJECTED","details":[{"app_code":1203,"reason":"InvalidQty"}]}},{"status":{"code":"REJECTED"}}]}`),
			Expect: []chain.ExpectResult{
				{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
				{Path: "results.1.status.code", Rule: "not_equal", Want: "SUCCESS", Got: "REJECTED", Passed: true},
				{Path: "results.1.status.details.0.app_code", Rule: "equals", Want: "1203", Got: "1203", Passed: true},
			}},
	}}
	row := store.ProposalRowOf(&store.Proposal{Chain: "batch", RunID: "run-1", ComparedTo: "run-0"}, rec)
	if row.Refusals != "REJECTED 1203 InvalidQty ×1" {
		t.Errorf("an asserted refused line counts once; an unasserted one does not: %+v", row)
	}
}
