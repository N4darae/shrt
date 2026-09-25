package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

const droppedError = "POST http://127.0.0.1:1/shrt.test.v1.ThingService/Fetch: sent, no answer: the backend closed the connection " +
	"before a response arrived (EOF): it most likely stopped or crashed while this request was in flight, so whether the call " +
	"took effect is unknown. This is not a verdict about the rpc: check the backend is up and run again"

func TestVerifyCountsADroppedStepAfterAChangeAsSentNotNotSent(t *testing.T) {
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)},
		{ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)},
		{ID: "list", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)},
	}}
	rec := runOf("run",
		stepAs("create", runner.StatusFailed, `{"n":2}`),
		stepAs("fetch", runner.StatusError, ""),
		stepAs("list", runner.StatusPassed, `{"n":1}`))
	rec.Steps[1].Error = droppedError
	report := diff.Compare(spot, rec)
	text := report.Text()
	if strings.Contains(text, "not sent") || strings.Contains(text, "[fetch] not_reached") {
		t.Fatalf("the dropped step went out, so it was sent:\n%s", text)
	}
	if !strings.Contains(text, "[fetch] status     status want=passed got=error") {
		t.Fatalf("the dropped step is a counted status change:\n%s", text)
	}
}

func TestDiffLabelsADroppedStepAsSentNotNotReached(t *testing.T) {
	a := runOf("a",
		stepAs("create", runner.StatusPassed, `{"n":1}`),
		stepAs("fetch", runner.StatusPassed, `{"n":1}`),
		stepAs("list", runner.StatusPassed, `{"n":1}`))
	b := runOf("b",
		stepAs("create", runner.StatusPassed, `{"n":1}`),
		stepAs("fetch", runner.StatusError, ""),
		stepAs("list", runner.StatusPassed, `{"n":1}`))
	b.Steps[1].Error = droppedError
	text := diff.CompareRuns(a, b).Text()
	if strings.Contains(text, "not reached in B: fetch") {
		t.Fatalf("the dropped step was sent in B, not unreached:\n%s", text)
	}
	if !strings.Contains(text, "sent in B, no answer (the connection closed): fetch") {
		t.Fatalf("the dropped step must be named as sent without an answer:\n%s", text)
	}
}
