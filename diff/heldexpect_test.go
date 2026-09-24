package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func heldExpectCase(addedHeld bool) *diff.Report {
	total := chain.ExpectResult{Path: "total", Rule: "equals", Want: 2, Got: 2, Passed: true}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":1}`)},
		{ID: "fetch", Call: "S/Fetch", Status: runner.StatusPassed, Response: []byte(`{"total":2,"qty":3}`), Expect: []chain.ExpectResult{total}},
	}}
	failed := total
	failed.Got, failed.Passed = 9, false
	added := chain.ExpectResult{Path: "qty", Rule: "equals", Want: 3, Got: 3, Passed: true}
	fetchExpect := []chain.ExpectResult{failed, added}
	fetchBody := `{"total":9,"qty":3}`
	if !addedHeld {
		added.Got, added.Passed = 4, false
		fetchExpect = []chain.ExpectResult{total, added}
		fetchBody = `{"total":2,"qty":4}`
	}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":7}`)},
		{ID: "fetch", Call: "S/Fetch", Status: runner.StatusFailed, Response: []byte(fetchBody), Expect: fetchExpect},
	}}
	now := &chain.Chain{Name: "c", Steps: []*chain.Step{
		{ID: "create", Call: "S/Create"},
		{ID: "fetch", Call: "S/Fetch", Expect: []chain.Expectation{
			{Path: "total", Equals: 2},
			{Path: "qty", Equals: 3},
		}},
	}}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = diff.ChainChanges(spot, now)
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{})
	return rep
}

func TestAnAddedExpectationThatHeldExplainsNothing(t *testing.T) {
	rep := heldExpectCase(true)
	text := rep.Text()
	if len(rep.RequestChanges) != 1 || rep.RequestChanges[0].Path != diff.ExpectPath {
		t.Fatalf("the only chain edit is the added expectation: %+v", rep.RequestChanges)
	}
	if got, all := len(rep.Unexplained()), len(rep.Changes); got != all || all == 0 {
		t.Fatalf("an added expectation that held explains none of the %d change(s), %d unexplained:\n%s", all, got, text)
	}
	for _, bad := range []string{"input differs", "after different input", "different input does not explain"} {
		if strings.Contains(text, bad) {
			t.Errorf("an expectation edit is not different input, the report must not say %q:\n%s", bad, text)
		}
	}
	if !strings.Contains(text, "evidence of a backend regression") {
		t.Errorf("the changes are evidence of a regression:\n%s", text)
	}
}

func TestAnAddedExpectationThatFailedExplainsItsOwnStatusChange(t *testing.T) {
	rep := heldExpectCase(false)
	text := rep.Text()
	for _, c := range rep.Changes {
		if c.Step == "fetch" && c.Kind == diff.KindStatus && !c.WithInput {
			t.Errorf("the added expectation failed, so it explains fetch's status change:\n%s", text)
		}
	}
	if strings.Contains(text, "input differs") || strings.Contains(text, "after different input") {
		t.Errorf("an expectation edit is not different input:\n%s", text)
	}
}
