package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestTheNextCommandAsksForAFreshValueOfAnInterpolatedVar(t *testing.T) {
	res := &chain.SliceResult{
		Source: "orders", Target: "create_order", Mode: chain.SliceModeClosure,
		Chain: &chain.Chain{
			Vars: map[string]any{"tag": "T0", "qty": 2},
			Steps: []*chain.Step{
				{ID: "create_product", Body: map[string]any{"sku": "SKU-${vars.tag}", "qty": "${vars.qty}"}},
			},
		},
	}
	got := keepWritesCommand(res, "r1", sliceVerifyArgs{vars: map[string]any{"qty": 3}}, []string{"add_stock"})
	if !strings.Contains(got, "-var tag=<fresh>") {
		t.Fatalf("tag makes created names unique and the source run used it, so pasting it again collides: %s", got)
	}
	if !strings.Contains(got, "-var qty=3") {
		t.Fatalf("a var used as a whole value keeps the value the user gave: %s", got)
	}
}
