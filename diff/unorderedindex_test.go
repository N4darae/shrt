package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestAChangedItemOfAnUnorderedListNamesItsIndexInTheReplay(t *testing.T) {
	a := `{"id_product":"prd-aaa111","name":"A","price_minor":"100"}`
	b := `{"id_product":"prd-bbb222","name":"B","price_minor":"200"}`
	moved := `{"id_product":"prd-aaa111","name":"A","price_minor":"101"}`
	step := func(list string) []*runner.StepRecord {
		return []*runner.StepRecord{{ID: "list", Call: "S/ListProducts", Status: runner.StatusPassed, Unordered: []string{"products"},
			Response: []byte(`{"products":[` + list + `]}`)}}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: step(a + "," + b)}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: step(b + "," + moved)}
	rep := diff.CompareMasking(spot, rec, nil)
	if len(rep.Changes) != 1 {
		t.Fatalf("one item changed: %v", rep.Changes)
	}
	text := rep.Text()
	if !strings.Contains(text, "products.1") {
		t.Fatalf("the changed item sits at products.1 in the replay; say so:\n%s", text)
	}
	if !strings.Contains(text, "products.0.price_minor") {
		t.Fatalf("keep the safe spot's index too, so both are named:\n%s", text)
	}
}
