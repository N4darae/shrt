package runner_test

import (
	"context"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestANotAsPinnedRunNamesTheUnpinnedFailureAsTheFinding(t *testing.T) {
	body := `{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`
	plain := map[string]any{"id_product": "p-1", "qty": "1"}
	for _, tc := range []struct {
		name string
		c    *chain.Chain
		want string
	}{
		{"an unpinned failure", threeStepKeptRed(7, 5, plain, chain.Pin{Step: "second", Path: "qty_on_hand"}),
			"NEW FAILURE outside the pinned defect: first qty_on_hand want=6 got=5"},
		{"two unpinned failures", threeStepKeptRed(7, 7, plain, chain.Pin{Step: "second", Path: "qty_on_hand"}),
			"NEW FAILURE outside the pinned defect: first qty_on_hand want=6 got=5; third qty_on_hand want=7 got=5"},
		{"a pinned path that held is no new failure", threeStepKeptRed(5, 5, plain, chain.Pin{Step: "first", Path: "status.code"}),
			"NEW FAILURE outside the pinned defect: first qty_on_hand want=6 got=5"},
		{"only a pin that differs", threeStepKeptRed(5, 5, plain, chain.Pin{Step: "first", Path: "qty_on_hand", Got: ptr("4")}),
			""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := rawStockRunner(t, body)
			rec, err := r.Run(context.Background(), normalized(t, tc.c), runner.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if rec.KeptRed != runner.KeptRedNotAsPinned {
				t.Fatalf("kept_red verdict %q (note %q)", rec.KeptRed, rec.KeptRedNote)
			}
			if rec.KeptRedNew != tc.want {
				t.Fatalf("finding %q, want %q (note %q)", rec.KeptRedNew, tc.want, rec.KeptRedNote)
			}
		})
	}
}

func ptr(s string) *string { return &s }
