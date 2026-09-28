package main

import (
	"context"
	"strings"
	"testing"
)

func TestSliceNamesTheKeptStepsAndWhyOnlyWithV(t *testing.T) {
	shop := newFakeShop()
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/scratch/probe-orders.yaml", cancelConfirmedChain)
	slice := func(args ...string) string {
		var err error
		out := captureStdout(t, func() {
			err = chainSlice(context.Background(), append([]string{".shrt/scratch/probe-orders.yaml", "-step", "cancel_confirmed", "-write", "probe-slice"}, args...))
		})
		if err != nil {
			t.Fatalf("slice: %v\n%s", err, out)
		}
		return out
	}
	out := slice()
	if strings.Contains(out, "changes the state of") || strings.Contains(out, "dropped write steps:") {
		t.Fatalf("the per-step table is for -v:\n%s", out)
	}
	if !strings.Contains(out, "kept 5 of 10 steps, dropped 5, writes among them create_product_2, add_stock, retry_single and 2 more: WARNING possible under-inclusion") {
		t.Fatalf("one line counts what was kept and dropped and names the dropped writes, capped:\n%s", out)
	}
	out = slice("-v")
	if !strings.Contains(out, "changes the state of what create_order_single created, which cancel_confirmed reads") || !strings.Contains(out, "dropped write steps:") {
		t.Fatalf("-v prints each kept step with its reason and the dropped writes:\n%s", out)
	}
}
