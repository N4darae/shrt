package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestANotAsPinnedNoteGroupsFailuresAndUnsentSteps(t *testing.T) {
	c := &chain.Chain{Name: "kept-red", KeptRed: []chain.Pin{{Step: "second", Path: "qty_on_hand"}}, Steps: []*chain.Step{
		{ID: "first", Call: "shop.catalog.v1.StockService/AddStock", Body: map[string]any{"id_product": "p-1", "qty": "1"},
			Expect: []chain.Expectation{{Path: "status.code", Equals: "REJECTED"}, {Path: "qty_on_hand", Equals: 9}}},
	}}
	for _, id := range []string{"second", "third", "fourth", "fifth"} {
		c.Steps = append(c.Steps, &chain.Step{ID: id, Call: "shop.catalog.v1.StockService/AddStock",
			Body:   map[string]any{"id_product": "${first.qty_on_hand}", "qty": "1"},
			Expect: []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}, {Path: "qty_on_hand", Equals: 1}}})
	}
	r := rawStockRunner(t, `{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`)
	rec, err := r.Run(context.Background(), normalized(t, c), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	note := rec.KeptRedNote
	if strings.Count(note, `"first"`) != 1 {
		t.Fatalf("a step failing on two unpinned paths is named once: %s", note)
	}
	if !strings.Contains(note, `step "second" was not sent`) || !strings.Contains(note, "3 other steps were not sent (third, fourth, fifth)") {
		t.Fatalf("unsent steps are grouped, the pinned one apart: %s", note)
	}
}
