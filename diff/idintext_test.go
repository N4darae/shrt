package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func idInTextSteps(product, message string) []*runner.StepRecord {
	return []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"product":{"id_product":"` + product + `"}}`)},
		{ID: "confirm", Call: "S/Confirm", Status: runner.StatusPassed, Response: []byte(`{"status":{"message":"` + message + `"}}`)},
	}
}

func TestARenamedIDInsideAMessageIsNotAChange(t *testing.T) {
	for _, tc := range []struct {
		name, was, now string
		changed        bool
	}{
		{"only the id", "not enough stock for prd-847c0a1b", "not enough stock for prd-b7703c2d", false},
		{"the text too", "not enough stock for prd-847c0a1b", "only 2 left for prd-b7703c2d", true},
		{"an id the renaming did not map", "not enough stock for prd-11110000", "not enough stock for prd-22220000", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: idInTextSteps("prd-847c0a1b", tc.was)}
			rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: idInTextSteps("prd-b7703c2d", tc.now)}
			rep := diff.CompareMasking(spot, rec, nil)
			if changed := !rep.Clean(); changed != tc.changed {
				t.Fatalf("verify: changed=%v, want %v\n%s", changed, tc.changed, rep.Text())
			}
			a := &runner.Record{RunID: "a", Chain: "c", Status: runner.StatusPassed, Steps: spot.Steps}
			runs := diff.CompareRuns(a, rec)
			if changed := len(runs.Changes) > 0; changed != tc.changed {
				t.Fatalf("diff: changed=%v, want %v\n%s", changed, tc.changed, runs.Text())
			}
			if !tc.changed && !strings.Contains(rep.MaskedList(), "id- or timestamp-shaped") {
				t.Fatalf("the renamed id is counted with the masked ids:\n%s", rep.Text())
			}
		})
	}
}

func TestARenamedIDInsideAMessageStaysMaskedWhenAListElsewhereBreaksTheRenaming(t *testing.T) {
	steps := func(product, message, listed string) []*runner.StepRecord {
		return append(idInTextSteps(product, message),
			&runner.StepRecord{ID: "list", Call: "S/List", Status: runner.StatusPassed, Response: []byte(`{"products":[{"id_product":"` + listed + `"}]}`)})
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: steps("prd-847c0a1b", "no product prd-847c0a1b-unknown", "prd-847c0a1b")}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: steps("prd-b7703c2d", "no product prd-b7703c2d-unknown", "prd-99990000")}
	rep := diff.CompareMasking(spot, rec, nil)
	a := &runner.Record{RunID: "a", Chain: "c", Status: runner.StatusPassed, Steps: spot.Steps}
	runs := diff.CompareRuns(a, rec)
	for name, changes := range map[string][]diff.Change{"verify": rep.Changes, "diff": runs.Changes} {
		var list, message bool
		for _, c := range changes {
			list = list || c.Step == "list"
			message = message || c.Step == "confirm"
		}
		if (name == "verify" && !list) || message {
			t.Fatalf("%s: list reported=%v, message reported=%v (want false): %+v", name, list, message, changes)
		}
	}
}
