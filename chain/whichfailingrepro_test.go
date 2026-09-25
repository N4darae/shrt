package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestWhichCodeReproducesTheFailingStepNotAPassingOne(t *testing.T) {
	c := &chain.Chain{Name: "catalog-refusals", Steps: []*chain.Step{
		{ID: "create_product_as_clerk", Call: "pkg.Catalog/CreateProduct",
			Expect: []chain.Expectation{{Path: "status.details.0.app_code", Equals: 1603}}},
		{ID: "add_stock_as_clerk", Call: "pkg.Catalog/AddStock",
			Expect: []chain.Expectation{{Path: "status.details.0.app_code", Equals: 1603}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	refused := map[string]any{"status": map[string]any{"code": "REJECTED", "details": []any{map[string]any{"app_code": float64(1603)}}}}
	accepted := map[string]any{"status": map[string]any{"code": "SUCCESS"}}
	hits := chain.Which([]*chain.Chain{c}, chain.WhichQuery{Code: "1603"}, chain.WhichOptions{
		Observations: func(string) []chain.Observation {
			return []chain.Observation{
				{Run: "r1", Step: "create_product_as_clerk", Status: "passed", Reached: true, Response: refused},
				{Run: "r1", Step: "add_stock_as_clerk", Status: "failed", Reached: true, Response: accepted,
					Failures: []chain.ExpectResult{{Path: "status.details.0.app_code", Rule: "equals", Want: 1603, Passed: false}}},
			}
		},
	})
	if len(hits) != 1 {
		t.Fatalf("one chain asserts 1603, got %d", len(hits))
	}
	if hits[0].Best != "add_stock_as_clerk" || !strings.Contains(hits[0].Command, "-step add_stock_as_clerk") {
		t.Fatalf("add_stock_as_clerk FAILED in the newest run, so the reproduce line slices it, not the passing create_product_as_clerk: best %s, %q", hits[0].Best, hits[0].Command)
	}
}
