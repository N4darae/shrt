package main

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestSliceVerdictMasksIDsAndFixturesInsideObjectOperands(t *testing.T) {
	verdict := func(path string, want any) chain.Verdict {
		return chain.Verdict{Step: "list", Status: "failed", Expect: []chain.ExpectResult{
			{Path: path, Rule: "includes", Want: want, Got: 0},
		}}
	}
	same := sameUpToFixtures(map[string]any{"tag": "xz1"}, map[string]any{"tag": "xz2"})
	for _, tc := range []struct {
		name, path   string
		source, echo any
		alike        bool
	}{
		{"fresh id", "products", map[string]any{"id_product": "prd-38bc1a2b3c4d"}, map[string]any{"id_product": "prd-64b2e5f6a7b8"}, true},
		{"fixture name", "products", map[string]any{"sku": "sku-xz1-2", "qty": float64(2)}, map[string]any{"sku": "sku-xz2-2", "qty": 2}, true},
		{"one_of list", "product.id_product", []any{"prd-38bc1a2b3c4d", "prd-11aa22bb33cc"}, []any{"prd-64b2e5f6a7b8", "prd-44dd55ee66ff"}, true},
		{"another value", "products", map[string]any{"id_product": "prd-38bc1a2b3c4d", "name": "a"}, map[string]any{"id_product": "prd-64b2e5f6a7b8", "name": "b"}, false},
		{"another key", "products", map[string]any{"id_product": "prd-38bc1a2b3c4d"}, map[string]any{"id_order": "prd-64b2e5f6a7b8"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diffs := chain.CompareVerdictsMasking(verdict(tc.path, tc.source), verdict(tc.path, tc.echo), same)
			if (len(diffs) == 0) != tc.alike {
				t.Fatalf("alike=%v, differences: %v", tc.alike, diffs)
			}
		})
	}
}
