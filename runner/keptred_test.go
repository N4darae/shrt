package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func keptRedChain(firstWant, secondWant int, pins ...chain.Pin) *chain.Chain {
	return &chain.Chain{Name: "kept-red", KeptRed: pins, Steps: []*chain.Step{
		{ID: "first", Call: "shop.catalog.v1.StockService/AddStock", Body: map[string]any{"id_product": "p-1", "qty": "1"},
			Expect: []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}, {Path: "qty_on_hand", Equals: firstWant}}},
		{ID: "second", Call: "shop.catalog.v1.StockService/AddStock", Body: map[string]any{"id_product": "p-1", "qty": "1"},
			Expect: []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}, {Path: "qty_on_hand", Equals: secondWant}}},
	}}
}

func strp(s string) *string { return &s }

func TestKeptRedHoldsOnlyWhenTheChainFailsExactlyAsPinned(t *testing.T) {
	body := `{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`
	for _, tc := range []struct {
		name        string
		c           *chain.Chain
		verdict     string
		noteSnippet string
	}{
		{"as pinned", keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qty_on_hand"}), runner.KeptRedAsPinned, "failed exactly"},
		{"as pinned with got", keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qty_on_hand", Got: strp("5")}), runner.KeptRedAsPinned, "got=5"},
		{"fails earlier", keptRedChain(6, 7, chain.Pin{Step: "second", Path: "qty_on_hand"}), runner.KeptRedNotAsPinned, `step "first" failed where nothing is pinned`},
		{"fails differently", keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qty_on_hand", Got: strp("4")}), runner.KeptRedNotAsPinned, "not the pinned got=4"},
		{"pinned path held", keptRedChain(5, 7, chain.Pin{Step: "second", Path: "status.code"}), runner.KeptRedNotAsPinned, `failed where nothing is pinned`},
		{"defect gone", keptRedChain(5, 5, chain.Pin{Step: "second", Path: "qty_on_hand"}), runner.KeptRedGone, "is gone"},
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
			if !strings.Contains(rec.KeptRedNote, tc.noteSnippet) {
				t.Fatalf("note %q does not say %q", rec.KeptRedNote, tc.noteSnippet)
			}
		})
	}
}

func TestKeptRedMustNameARealStepAndExpectation(t *testing.T) {
	for _, pin := range []chain.Pin{{Step: "nope", Path: "qty_on_hand"}, {Step: "second", Path: "qty"}} {
		c := keptRedChain(5, 7, pin)
		if err := c.Normalize(); err == nil || !strings.Contains(err.Error(), "kept_red") {
			t.Fatalf("pin %+v must be refused at load, got %v", pin, err)
		}
	}
}

func TestADryRunOfAKeptRedChainHasNoKeptRedVerdict(t *testing.T) {
	r := rawStockRunner(t, `{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`)
	rec, err := r.Run(context.Background(), normalized(t, keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qty_on_hand"})), runner.Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.KeptRed != "" {
		t.Fatalf("a dry run sends nothing and cannot show the defect, got %q", rec.KeptRed)
	}
}

func TestAKeptRedPinPathMatchesItsExpectationWhateverTheCase(t *testing.T) {
	c := keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qtyOnHand"})
	r := rawStockRunner(t, `{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`)
	rec, err := r.Run(context.Background(), normalized(t, c), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.KeptRed != runner.KeptRedAsPinned {
		t.Fatalf("qtyOnHand pins qty_on_hand, as an expectation path would read it: %q %s", rec.KeptRed, rec.KeptRedNote)
	}
}
