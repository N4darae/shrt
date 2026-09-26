package chain_test

import (
	"fmt"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func clockStep() *chain.Step {
	return &chain.Step{ID: "create", Expect: []chain.Expectation{
		{Path: "status.code", Equals: "SUCCESS"},
		{Path: "product.created_at", Within: &chain.Within{Of: "${nowunix}", By: 300}},
		{Path: "product.created_at", Gte: "${nowunix-300}"},
	}}
}

func clockVerdict(now float64, got any) chain.Verdict {
	within := map[string]any{"of": fmt.Sprintf("%.0f", now), "by": 300}
	return chain.Verdict{Step: "create", Status: "failed", ErrorCode: "SUCCESS", Expect: []chain.ExpectResult{
		{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
		{Path: "product.created_at", Rule: "within", Want: within, Got: got},
		{Path: "product.created_at", Rule: "gte", Want: fmt.Sprintf("%.0f", now-300), Got: got},
	}}
}

func TestSliceVerdictReResolvesAClockRelativeBound(t *testing.T) {
	step := clockStep()
	source, replay := clockVerdict(1790325508, nil), clockVerdict(1790325514, nil)
	if d := chain.CompareVerdicts(source, replay); len(d) == 0 {
		t.Fatalf("the resolved bounds differ, which the plain comparison reports: %v", d)
	}
	if d := chain.CompareVerdicts(chain.ClockRelative(step, source), chain.ClockRelative(step, replay)); len(d) != 0 {
		t.Fatalf("a bound resolved from ${nowunix} at each run's own time is the same bound: %v", d)
	}

	source, replay = clockVerdict(1790325508, "1790329108"), clockVerdict(1790325514, "1790329115")
	alike := func(_ string, a, b any) bool { return chain.SameClockOffset(a, b) }
	if d := chain.CompareVerdictsMasking(chain.ClockRelative(step, source), chain.ClockRelative(step, replay), alike); len(d) != 0 {
		t.Fatalf("a stamp an hour ahead of each run's clock fails the same way in both: %v", d)
	}
	replay = clockVerdict(1790325514, "1790325000")
	if d := chain.CompareVerdictsMasking(chain.ClockRelative(step, source), chain.ClockRelative(step, replay), alike); len(d) == 0 {
		t.Fatal("a stamp an hour ahead in one run and minutes behind in the other is a different failure")
	}
}
