package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func threeStepKeptRed(secondWant, thirdWant int, thirdBody map[string]any, pins ...chain.Pin) *chain.Chain {
	c := keptRedChain(6, secondWant, pins...)
	c.Steps = append(c.Steps, &chain.Step{ID: "third", Call: "shop.catalog.v1.StockService/AddStock", Body: thirdBody,
		Expect: []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}, {Path: "qty_on_hand", Equals: thirdWant}}})
	return c
}

func TestAKeptRedChainRunsPastItsPinnedFailures(t *testing.T) {
	body := `{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`
	plain := map[string]any{"id_product": "p-1", "qty": "1"}
	for _, tc := range []struct {
		name    string
		c       *chain.Chain
		verdict string
		snippet string
	}{
		{"every pin is evaluated", threeStepKeptRed(7, 5, plain,
			chain.Pin{Step: "first", Path: "qty_on_hand"}, chain.Pin{Step: "second", Path: "qty_on_hand"}),
			runner.KeptRedAsPinned, "failed exactly"},
		{"a later regression is seen", threeStepKeptRed(5, 7, plain, chain.Pin{Step: "first", Path: "qty_on_hand"}),
			runner.KeptRedNotAsPinned, `step "third" failed where nothing is pinned`},
		{"a step left unexercised is not as pinned", threeStepKeptRed(5, 5, map[string]any{"id_product": "${first.qty_on_hand}", "qty": "1"},
			chain.Pin{Step: "first", Path: "qty_on_hand"}),
			runner.KeptRedNotAsPinned, `step "third" was not sent`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := rawStockRunner(t, body)
			rec, err := r.Run(context.Background(), normalized(t, tc.c), runner.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if rec.KeptRed != tc.verdict {
				t.Fatalf("kept_red verdict %q, want %q (status %s, note %q)", rec.KeptRed, tc.verdict, rec.Status, rec.KeptRedNote)
			}
			if !strings.Contains(rec.KeptRedNote, tc.snippet) {
				t.Fatalf("note %q does not say %q", rec.KeptRedNote, tc.snippet)
			}
		})
	}
}
