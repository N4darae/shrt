package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestChainChangesNameAWholeRemovedBatchItem(t *testing.T) {
	spot := &store.SafeSpot{Chain: "batch", Steps: []*runner.StepRecord{{
		ID: "stock", Call: "StockService/AddStockBatch",
		Request: []byte(`{"lines":[{"id_product":"p-a","qty":"10"},{"id_product":"p-b","qty":"20"},{"id_product":"p-a","qty":"5"}]}`),
		BodyRefs: map[string]string{
			"lines.0.id_product": "${a.id}", "lines.1.id_product": "${b.id}", "lines.2.id_product": "${a.id}",
		},
	}}}
	c := &chain.Chain{Name: "batch", Steps: []*chain.Step{{
		ID: "stock", Call: "StockService/AddStockBatch",
		Body: map[string]any{"lines": []any{
			map[string]any{"id_product": "${a.id}", "qty": "10"},
			map[string]any{"id_product": "${b.id}", "qty": "20"},
		}},
	}}}
	changes := diff.ChainChanges(spot, c)
	if len(changes) != 1 {
		t.Fatalf("want one change, the removed item, got %+v", changes)
	}
	got := changes[0].Path + " " + changes[0].Transition()
	for _, want := range []string{"body.lines.2 ", `"id_product":"${a.id}"`, `"qty":"5"`, "-> absent"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the removed item is named whole, qty included, want %q in %q", want, got)
		}
	}
}
