package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestAReadBackHeldBehindAFailedStepShowsWhatItAnswered(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "batch", Call: "S/Batch", Status: runner.StatusPassed, Response: []byte(`{"qty":12}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusPassed, Response: []byte(`{"qty":12}`),
			Expect: []chain.ExpectResult{{Path: "qty", Rule: "equals", Want: 12, Got: 12, Passed: true}}},
	}}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		{ID: "batch", Call: "S/Batch", Status: runner.StatusFailed, Response: []byte(`{"qty":6}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusFailed, Response: []byte(`{"qty":12}`),
			Expect: []chain.ExpectResult{{Path: "qty", Rule: "unevaluated", Want: 6, Got: 12,
				Detail: `not evaluated: ${batch.qty} reads step "batch", which did not pass, so its value is not evidence (-keep-going)`}}},
	}}
	text := diff.CompareMasking(spot, rec, nil).Text()
	if !strings.Contains(text, "answered qty=12, not judged: it reads step batch, which did not pass") {
		t.Fatalf("the held read-back was sent, so its status change shows what it answered:\n%s", text)
	}
}

func TestAHeldReadBackNamesTheExpectationNotJudged(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"at":"t"}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusPassed, Response: []byte(`{"qty":1}`)},
	}}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusFailed, Response: []byte(`{}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusFailed, Response: []byte(`{"qty":1}`),
			Expect: []chain.ExpectResult{
				{Path: "qty", Rule: "equals", Want: 1, Got: 1, Passed: true},
				{Path: "at", Rule: "unevaluated", Detail: `not evaluated: ${create.at} reads step "create", which did not pass, so its value is not evidence (-keep-going)`}}},
	}}
	text := diff.CompareMasking(spot, rec, nil).Text()
	if !strings.Contains(text, "(at not judged: it reads step create, which did not pass)") {
		t.Fatalf("a sent read-back names the held expectation, not the whole step, as not judged:\n%s", text)
	}
}
