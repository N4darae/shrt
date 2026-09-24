package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestARemovedMiddleStepIsOneChangeAndTheRestAreStillCompared(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":1}`)},
		{ID: "fetch", Call: "S/Fetch", Status: runner.StatusPassed, Response: []byte(`{"n":2}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusPassed, Response: []byte(`{"n":3}`)},
		{ID: "list", Call: "S/List", Status: runner.StatusPassed, Response: []byte(`{"n":4}`)},
	}}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":1}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusPassed, Response: []byte(`{"n":3}`)},
		{ID: "list", Call: "S/List", Status: runner.StatusPassed, Response: []byte(`{"n":5}`)},
	}}
	now := &chain.Chain{Name: "c", Steps: []*chain.Step{{ID: "create", Call: "S/Create"}, {ID: "get", Call: "S/Get"}, {ID: "list", Call: "S/List"}}}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = diff.ChainChanges(spot, now)
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{})
	kinds := []string{}
	for _, c := range rep.Changes {
		kinds = append(kinds, c.Step+":"+c.Kind+":"+c.Path)
	}
	got := strings.Join(kinds, ",")
	if got != "fetch:missing:step,list:changed:n" {
		t.Fatalf("the removed step is one change, get did not move, and list is still compared at its own record: %s\n%s", got, rep.Text())
	}
	text := rep.Text()
	if strings.Contains(text, "request value(s)") {
		t.Fatalf("a removed step is a chain change, not a request value:\n%s", text)
	}
	if !strings.Contains(text, "1 chain change(s)") {
		t.Fatalf("count the chain change as one:\n%s", text)
	}
}
