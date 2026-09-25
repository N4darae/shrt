package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestAChangedExpectationNeverExplainsATransportError(t *testing.T) {
	for _, rule := range []string{"equals", "unevaluated"} {
		spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
			{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":1}`)},
			{ID: "fetch", Call: "S/Fetch", Status: runner.StatusPassed, Response: []byte(`{"qty":3}`),
				Expect: []chain.ExpectResult{{Path: "qty", Rule: "equals", Want: 3, Got: 3, Passed: true}}},
		}}
		rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
			{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":7}`)},
			{ID: "fetch", Call: "S/Fetch", Status: runner.StatusFailed, Response: []byte(`{"code":"internal","message":"pool exhausted"}`),
				Transport: &runner.TransportError{Code: "internal", Message: "pool exhausted"},
				Expect:    []chain.ExpectResult{{Path: "qty", Rule: rule, Want: 4, Passed: false, Detail: "path not present in response"}}},
		}}
		now := &chain.Chain{Name: "c", Steps: []*chain.Step{
			{ID: "create", Call: "S/Create"},
			{ID: "fetch", Call: "S/Fetch", Expect: []chain.Expectation{{Path: "qty", Equals: 4}}},
		}}
		rep := diff.CompareMasking(spot, rec, nil)
		rep.RequestChanges = diff.ChainChangesIn(spot, now, rec)
		rep.SeparateInput(spot, rec, nil, diff.Fixtures{})
		text := rep.Text()
		for _, c := range rep.Changes {
			if c.Step == "fetch" && c.Kind == diff.KindStatus && c.WithInput {
				t.Errorf("rule %s: the step was refused at transport, which no expectation edit explains:\n%s", rule, text)
			}
		}
		if strings.Contains(text, "explained by the failed changed expectation") {
			t.Errorf("rule %s: a transport error is not explained by an expectation edit:\n%s", rule, text)
		}
	}
}
