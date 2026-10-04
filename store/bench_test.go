package store_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func bulkyRun(id string, steps, items int) *runner.Record {
	rec := &runner.Record{
		RunID: id, Chain: "bulky", Target: "http://localhost", Status: runner.StatusPassed,
		StartedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Vars: map[string]any{"tag": "t-1", "qty": int64(3)},
	}
	for s := range steps {
		list := make([]map[string]any, items)
		for i := range list {
			list[i] = map[string]any{
				"id_product": fmt.Sprintf("%08x-0000-4000-8000-%012d", s, i), "name": fmt.Sprintf("Widget <%d> & co", i),
				"price_minor": fmt.Sprint(100 + i), "created_at": "2026-10-01T12:00:00Z", "tags": []string{"a", "b"},
			}
		}
		response, _ := json.Marshal(map[string]any{"status": map[string]any{"code": "SUCCESS"}, "products": list})
		request, _ := json.Marshal(map[string]any{"name": "widget", "qty": "3", "sku": fmt.Sprintf("sku-%d", s)})
		rec.Steps = append(rec.Steps, &runner.StepRecord{
			Index: s + 1, ID: fmt.Sprintf("step_%d", s), Call: "ProductService/ListProducts", Status: runner.StatusPassed,
			LatencyMS: 4, Request: request, Response: response, BodyRefs: map[string]string{"sku": "${vars.tag}"},
			Expect: []chain.ExpectResult{{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true}},
		})
	}
	return rec
}

func BenchmarkLoadRun(b *testing.B) {
	root := b.TempDir()
	s := store.New(filepath.Join(root, "runs"), filepath.Join(root, "safespots"))
	rec := bulkyRun("20261001T120000Z-0000beef", 40, 60)
	if _, err := s.SaveRun(rec); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.LoadRun("bulky", rec.RunID); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSaveRun(b *testing.B) {
	root := b.TempDir()
	s := store.New(filepath.Join(root, "runs"), filepath.Join(root, "safespots"))
	rec := bulkyRun("20261001T120000Z-0000beef", 40, 60)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.SaveRun(rec); err != nil {
			b.Fatal(err)
		}
	}
}
