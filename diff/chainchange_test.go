package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestARemovedStepIsAChainChangeNotARegression(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"ok":true}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusPassed, Response: []byte(`{"ok":true}`)},
	}}
	now := &chain.Chain{Name: "c", Steps: []*chain.Step{{ID: "create", Call: "S/Create"}}}
	rec := runOf("run", stepAs("create", runner.StatusPassed, `{"ok":true}`))
	rec.Steps[0].Call = "S/Create"

	changes := diff.ChainChanges(spot, now)
	if len(changes) != 1 || changes[0].Step != "get" || changes[0].Kind != diff.KindMissing {
		t.Fatalf("the chain no longer has step get; that is a change of input: %+v", changes)
	}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = append(changes, rep.RequestChanges...)
	if rep.Clean() {
		t.Fatal("the step count still differs")
	}
	if !strings.Contains(rep.Text(), "after a chain change, so they are not evidence of a backend regression") {
		t.Fatalf("a removed step must be reported as a chain change:\n%s", rep.Text())
	}

	for name, c := range map[string]*chain.Chain{
		"added":     {Steps: []*chain.Step{{ID: "create", Call: "S/Create"}, {ID: "get", Call: "S/Get"}, {ID: "list", Call: "S/List"}}},
		"reordered": {Steps: []*chain.Step{{ID: "get", Call: "S/Get"}, {ID: "create", Call: "S/Create"}}},
		"recalled":  {Steps: []*chain.Step{{ID: "create", Call: "S/Create"}, {ID: "get", Call: "S/Fetch"}}},
	} {
		if got := diff.ChainChanges(spot, c); len(got) == 0 {
			t.Errorf("%s: the chain's steps differ from the confirmed run's, got no change", name)
		}
	}
	same := &chain.Chain{Steps: []*chain.Step{{ID: "create", Call: "S/Create"}, {ID: "get", Call: "S/Get"}}}
	if got := diff.ChainChanges(spot, same); len(got) != 0 {
		t.Errorf("an unchanged chain is no input change: %+v", got)
	}
}
