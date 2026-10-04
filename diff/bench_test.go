package diff_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func bulkyRun(id string, run, steps, items int) *runner.Record {
	at := time.Date(2026, 10, 1, 12, run, 0, 0, time.UTC)
	rec := &runner.Record{
		RunID: id, Chain: "bulky", Target: "http://localhost", Status: runner.StatusPassed, StartedAt: at,
		Volatile: []string{"**.created_at", "**.updated_at"}, Vars: map[string]any{"tag": fmt.Sprintf("t-%d", run)},
	}
	for s := range steps {
		list := make([]map[string]any, items)
		for i := range list {
			list[i] = map[string]any{
				"id_product": fmt.Sprintf("%08x-%04x-4000-8000-%012d", s, run, i), "name": fmt.Sprintf("Widget t-%d %d", run, i),
				"price_minor": fmt.Sprint(100 + i), "created_at": at.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
				"owner": map[string]any{"customer_id": fmt.Sprintf("cus-%d-%d", run, s), "note": fmt.Sprintf("made for cus-%d-%d", run, s)},
			}
		}
		response, _ := json.Marshal(map[string]any{"status": map[string]any{"code": "SUCCESS"}, "products": list, "total": items})
		request, _ := json.Marshal(map[string]any{"name": fmt.Sprintf("widget t-%d", run), "qty": "3", "sku": fmt.Sprintf("sku-t-%d-%d", run, s)})
		rec.Steps = append(rec.Steps, &runner.StepRecord{
			Index: s + 1, ID: fmt.Sprintf("step_%d", s), Call: "ProductService/ListProducts", Procedure: "/shop.v1.ProductService/ListProducts",
			Status: runner.StatusPassed, LatencyMS: 4, Request: request, Response: response,
			Expect: []chain.ExpectResult{{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true}},
		})
	}
	return rec
}

func BenchmarkCompareRuns(b *testing.B) {
	a, z := bulkyRun("run-a", 1, 20, 60), bulkyRun("run-b", 2, 20, 60)
	b.ReportAllocs()
	for b.Loop() {
		diff.CompareRunsSkipping(a, z, nil, diff.Fixtures{}).Text()
	}
}

func BenchmarkCompareSafeSpot(b *testing.B) {
	a, z := bulkyRun("run-a", 1, 20, 60), bulkyRun("run-b", 2, 20, 60)
	spot := &store.SafeSpot{Chain: a.Chain, RunID: a.RunID, Volatile: a.Volatile, Steps: a.Steps}
	b.ReportAllocs()
	for b.Loop() {
		rep := diff.CompareMasking(spot, z, nil)
		rep.SeparateInput(spot, z, nil, diff.Fixtures{})
		_ = rep.Text()
	}
}

func BenchmarkCompareGrownList(b *testing.B) {
	a, z := bulkyRun("run-a", 1, 2, 1500), bulkyRun("run-b", 2, 2, 1520)
	spot := &store.SafeSpot{Chain: a.Chain, RunID: a.RunID, Volatile: a.Volatile, Steps: a.Steps}
	b.ReportAllocs()
	for b.Loop() {
		rep := diff.CompareMasking(spot, z, nil)
		_ = rep.Text()
	}
}
