package runner_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestPinChangesNamesAPinWhoseGotMovedAndHoldsThroughUnevaluatedPins(t *testing.T) {
	got := func(s string) *string { return &s }
	c := &chain.Chain{Name: "k", Steps: []*chain.Step{{ID: "make"}, {ID: "read"}, {ID: "read_2"}},
		KeptRed: []chain.Pin{{Step: "read", Path: "level", Got: got("8")}, {Step: "read_2", Path: "state", Got: got("DONE")}}}
	rec := func(level string, second chain.ExpectResult) *runner.Record {
		return &runner.Record{KeptRed: runner.KeptRedNotAsPinned, Steps: []*runner.StepRecord{
			{ID: "make", Status: runner.StatusFailed, Expect: []chain.ExpectResult{{Path: "total", Rule: "equals", Want: "5", Got: "4"}}},
			{ID: "read", Status: runner.StatusFailed, Expect: []chain.ExpectResult{{Path: "level", Rule: "equals", Want: "10", Got: level}}},
			{ID: "read_2", Status: runner.StatusFailed, Expect: []chain.ExpectResult{second}},
		}}
	}
	unevaluated := chain.ExpectResult{Path: "state", Rule: "unevaluated"}
	if changed, held := runner.PinChanges(c, rec("8", unevaluated)); len(changed) != 0 || !held {
		t.Errorf("a pin unevaluated behind another failure and a pin that held: held, got %v %v", changed, held)
	}
	changed, held := runner.PinChanges(c, rec("-3", unevaluated))
	if changed["read level"] != "8" || held {
		t.Errorf("a pinned got that moved is named with its pinned value, got %v %v", changed, held)
	}
	if _, held := runner.PinChanges(c, rec("8", chain.ExpectResult{Path: "other", Rule: "equals", Want: "a", Got: "b", Passed: false})); held {
		t.Error("a pinned path that no longer fails does not hold")
	}
}
