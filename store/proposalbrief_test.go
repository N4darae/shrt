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
