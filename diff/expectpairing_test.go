package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestAnExpectationAddedInTheMiddleIsPairedByPath(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "confirm", Call: "S/Confirm", Status: runner.StatusPassed, Response: []byte(`{}`), Expect: []chain.ExpectResult{
			{Path: "status.code", Rule: "equals", Want: "SUCCESS", Passed: true},
			{Path: "order.status", Rule: "equals", Want: "CONFIRMED", Passed: true},
			{Path: "order.id_order", Rule: "equals", Want: "ord-d034498e2fa4", Passed: true},
		}},
	}}
	now := &chain.Chain{Name: "c", Steps: []*chain.Step{{ID: "confirm", Call: "S/Confirm", Expect: []chain.Expectation{
		{Path: "status.code", Equals: "SUCCESS"},
		{Path: "order.status", Equals: "CONFIRMED"},
		{Path: "order.lines.0.qty", Equals: "${vars.qty}"},
		{Path: "order.id_order", Equals: "${create_order.order.id_order}"},
	}}}}
	changes := diff.ChainChanges(spot, now)
	if len(changes) != 1 || changes[0].Kind != diff.KindUnexpected {
		t.Fatalf("one expectation was added; the others are unchanged: %+v", changes)
	}
	if !strings.Contains(changes[0].Transition(), "absent -> order.lines.0.qty equals") || strings.Contains(changes[0].Transition(), "order.id_order") {
		t.Fatalf("the change must be the added path alone: %s", changes[0].Transition())
	}

	rec := runOf("run", stepAs("confirm", runner.StatusPassed, `{}`))
	rec.Steps[0].Expect = []chain.ExpectResult{
		{Path: "status.code", Rule: "equals", Want: "SUCCESS", Passed: true},
		{Path: "order.status", Rule: "equals", Want: "CONFIRMED", Passed: true},
		{Path: "order.lines.0.qty", Rule: "equals", Want: 3, Passed: true},
		{Path: "order.id_order", Rule: "equals", Want: "ord-0f0f0f0f0f0f", Passed: true},
	}
	changes = diff.ChainChangesIn(spot, now, rec)
	if len(changes) != 1 || changes[0].Transition() != "absent -> order.lines.0.qty equals 3" {
		t.Fatalf("with the run at hand the added expectation shows its value as run, the form the safe spot records: %+v", changes)
	}

	rule := &chain.Chain{Name: "c", Steps: []*chain.Step{{ID: "confirm", Call: "S/Confirm", Expect: []chain.Expectation{
		{Path: "status.code", Equals: "SUCCESS"},
		{Path: "order.id_order", NotEmpty: true},
	}}}}
	changes = diff.ChainChanges(spot, rule)
	got := []string{}
	for _, c := range changes {
		got = append(got, c.Transition())
	}
	want := []string{"order.id_order equals ord-d034498e2fa4 -> order.id_order not_empty true", "order.status equals CONFIRMED -> absent"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("per path: a changed rule and a removed expectation\nwant %q\ngot  %q", want, got)
	}
}
