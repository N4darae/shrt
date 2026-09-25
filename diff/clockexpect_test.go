package diff_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func clockExpectCase(by any) []diff.Change {
	recorded := func(of int) []chain.ExpectResult {
		return []chain.ExpectResult{
			{Path: "expires_at", Rule: "within", Want: fmt.Sprintf("%d ± 10", of), Got: of, Passed: true},
			{Path: "created_at", Rule: "between", Want: fmt.Sprintf("[%d, %d]", of-60, of+60), Got: of, Passed: true},
			{Path: "qty", Rule: "gte", Want: "2", Got: 3, Passed: true},
			{Path: "request_id", Rule: "not_equal", Want: "0b3e", Got: "77aa", Passed: true},
		}
	}
	spot := &store.SafeSpot{Chain: "auth", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "login", Call: "S/Login", Status: runner.StatusPassed, Expect: recorded(1790314828)},
	}}
	rec := &runner.Record{RunID: "run", Chain: "auth", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "login", Call: "S/Login", Status: runner.StatusPassed, Expect: recorded(1790314900)},
	}}
	now := &chain.Chain{Name: "auth", Steps: []*chain.Step{{ID: "login", Call: "S/Login", Expect: []chain.Expectation{
		{Path: "expires_at", Within: &chain.Within{Of: "${nowunix+3600}", By: by}},
		{Path: "created_at", Between: []any{"${nowunix-60}", "${nowunix+60}"}},
		{Path: "request_id", NotEqual: "${uuid}"},
		{Path: "qty", Gte: 2},
	}}}}
	return diff.ChainChangesIn(spot, now, rec)
}

func TestAClockTemplateInAnExpectationIsNotAChainEdit(t *testing.T) {
	if got := clockExpectCase(10); len(got) != 0 {
		t.Fatalf("the chain is unchanged; only ${nowunix...} and ${uuid} resolved to other values, yet: %+v", got)
	}
}

func TestALiteralOperandBesideAClockTemplateIsStillCompared(t *testing.T) {
	got := clockExpectCase(20)
	if len(got) != 1 || got[0].Path != diff.ExpectPath || !strings.Contains(fmt.Sprint(got[0].Got), "expires_at within") {
		t.Fatalf("within.by changed from 10 to 20, which is a chain edit: %+v", got)
	}
}
