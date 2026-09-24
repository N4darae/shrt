package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestRunSummaryPrintsExportsAsJSON(t *testing.T) {
	rec := &runner.Record{
		Chain:  "orders",
		Status: runner.StatusPassed,
		Exports: map[string]any{
			"order": map[string]any{"id_order": "ord-1", "lines": []any{map[string]any{"id_product": "prd-1", "qty": 2}}},
			"qty":   5,
		},
	}
	out := summary(rec, false)
	if strings.Contains(out, "map[") {
		t.Fatalf("exports must not print in Go map syntax:\n%s", out)
	}
	for _, want := range []string{`order={"id_order":"ord-1","lines":[{"id_product":"prd-1","qty":2}]}`, "qty=5"} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %s in the summary:\n%s", want, out)
		}
	}
}

func TestDryRunSummarySaysExportsAreNotProduced(t *testing.T) {
	rec := &runner.Record{
		Chain:   "orders",
		Status:  runner.StatusPassed,
		DryRun:  true,
		Exports: map[string]any{"pid": "", "order": map[string]any{"id_order": ""}},
	}
	out := summary(rec, true)
	if strings.Contains(out, "pid=") || strings.Contains(out, "map[") {
		t.Fatalf("a dry run sends nothing, so it has no export value to print:\n%s", out)
	}
	if !strings.Contains(out, "not produced in a dry run") || !strings.Contains(out, "order, pid") {
		t.Fatalf("the summary must name the exports and say a dry run does not produce them:\n%s", out)
	}
}
