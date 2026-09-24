package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

const timedOutError = "POST http://127.0.0.1:1/shrt.test.v1.ThingService/Fetch: sent, no answer before target.timeout (700ms): context deadline exceeded"

func timedOutRun() *runner.Record {
	rec := runOf("run",
		stepAs("create", runner.StatusError, ""),
		stepAs("fetch", runner.StatusError, ""),
		stepAs("list", runner.StatusPassed, `{"n":1}`))
	rec.Steps[0].Error = timedOutError
	rec.Steps[1].Error = timedOutError
	return rec
}

func TestVerifyLabelsATimedOutStepAsSentNotNotSent(t *testing.T) {
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)},
		{ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)},
		{ID: "list", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)},
	}}
	text := diff.Compare(spot, timedOutRun()).Text()
	if strings.Contains(text, "not sent") {
		t.Fatalf("a step that went out and timed out was sent:\n%s", text)
	}
	if !strings.Contains(text, "[fetch] not_reached status want=passed got=error, sent, no answer before target.timeout") {
		t.Fatalf("the timed-out step must say it was sent and got no answer in time:\n%s", text)
	}
}

func TestDiffLabelsATimedOutStepAsSentNotNotReached(t *testing.T) {
	a := runOf("a",
		stepAs("create", runner.StatusPassed, `{"n":1}`),
		stepAs("fetch", runner.StatusPassed, `{"n":1}`),
		stepAs("list", runner.StatusPassed, `{"n":1}`))
	text := diff.CompareRuns(a, timedOutRun()).Text()
	if strings.Contains(text, "not reached in B: create") || strings.Contains(text, "not reached in B: fetch") {
		t.Fatalf("a timed-out step was sent in B, not unreached:\n%s", text)
	}
	if !strings.Contains(text, "sent in B, no answer before target.timeout: create, fetch") {
		t.Fatalf("the timed-out steps must be named as sent without an answer:\n%s", text)
	}
}
