package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAKeptRedChainRunsPastAnUnpinnedFailure(t *testing.T) {
	body := `{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`
	plain := map[string]any{"id_product": "p-1", "qty": "1"}
	reads := map[string]any{"id_product": "${first.qty_on_hand}", "qty": "1"}
	for _, tc := range []struct {
		name   string
		c      *chain.Chain
		sent   string
		status string
		says   []string
	}{
		{"the pin after an unpinned failure is evaluated", threeStepKeptRed(7, 5, plain, chain.Pin{Step: "second", Path: "qty_on_hand"}),
			"second", runner.StatusFailed, []string{`step "first" failed where nothing is pinned`}},
		{"a pinned step behind a failed step is not sent", threeStepKeptRed(5, 7, reads, chain.Pin{Step: "third", Path: "qty_on_hand"}),
			"third", runner.StatusSkipped, []string{`step "first" failed where nothing is pinned`, `step "third" was not sent`, "its pinned failure was not seen"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := rawStockRunner(t, body)
			rec, err := r.Run(context.Background(), normalized(t, tc.c), runner.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if rec.KeptRed != runner.KeptRedNotAsPinned {
				t.Fatalf("kept_red verdict %q, want %q (note %q)", rec.KeptRed, runner.KeptRedNotAsPinned, rec.KeptRedNote)
			}
			sr, ok := rec.Step(tc.sent)
			if !ok || sr.Status != tc.status {
				t.Fatalf("a kept_red chain runs every step as -keep-going does: step %q want %s, got %+v", tc.sent, tc.status, sr)
			}
			for _, s := range tc.says {
				if !strings.Contains(rec.KeptRedNote, s) {
					t.Errorf("note %q does not say %q", rec.KeptRedNote, s)
				}
			}
			if strings.Contains(rec.KeptRedNote, "never answered") {
				t.Errorf("every step was reached, so none was never answered: %q", rec.KeptRedNote)
			}
		})
	}
}
