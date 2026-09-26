package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAKeptRedNoteOnAPinnedStepThatPassedReadsWithOneBut(t *testing.T) {
	r := rawStockRunner(t, `{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`)
	c := keptRedChain(5, 7, chain.Pin{Step: "first", Path: "qty_on_hand"}, chain.Pin{Step: "second", Path: "qty_on_hand"})
	rec, err := r.Run(context.Background(), normalized(t, c), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.KeptRed != runner.KeptRedNotAsPinned {
		t.Fatalf("first passed although pinned, want %q, got %q: %s", runner.KeptRedNotAsPinned, rec.KeptRed, rec.KeptRedNote)
	}
	if !strings.Contains(rec.KeptRedNote, `step "first" passed although it is pinned failing on qty_on_hand`) {
		t.Fatalf("the note names the pinned step that passed: %s", rec.KeptRedNote)
	}
	if !strings.Contains(rec.KeptRedNote, "another regression on the same record; compare with shrt diff") {
		t.Fatalf("a pinned step that passes may be a fix or another regression, and the note says how to tell: %s", rec.KeptRedNote)
	}
	if strings.Count(rec.KeptRedNote, ", but ") != 1 {
		t.Fatalf("the note says but once: %s", rec.KeptRedNote)
	}
}
