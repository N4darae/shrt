package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestVerifyReportsStepsSkippedBehindAFailureAsNotReachedNotAsALengthChange(t *testing.T) {
	spot := spotOf(nil, step("create", `{"id":"t-1"}`), step("fetch", `{"name":"w"}`), step("list", `{"n":1}`))
	failed := step("create", `{"id":"t-1","error":{"code":"REJECTED"}}`)
	failed.Status, failed.Error = runner.StatusFailed, "expectation failed"
	skipped := &runner.StepRecord{Index: 2, ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusSkipped, Error: `not sent: ${create.id} reads step "create"`}
	rec := recOf(failed, skipped, step("list", `{"n":1}`))
	rec.Status, rec.KeepGoing = runner.StatusFailed, true

	rep := diff.Compare(spot, rec)
	text := rep.Text()
	if strings.Contains(text, "length") {
		t.Fatalf("a keep-going run records every step, so nothing changed length:\n%s", text)
	}
	if !strings.Contains(text, "[fetch] not_reached") || !strings.Contains(text, "first failing step: step 1 create") {
		t.Fatalf("the skipped step is not reached and the first red is named:\n%s", text)
	}
	for _, c := range rep.Changes {
		if c.Step == "fetch" && c.Kind != diff.KindNotReached {
			t.Fatalf("an unsent step has no response to compare, got %+v", c)
		}
	}
}

func TestVerifyOfARecordedRunThatStoppedEarlyReportsTheRestAsNotReached(t *testing.T) {
	spot := spotOf(nil, step("create", `{"id":"t-1"}`), step("fetch", `{"name":"w"}`), step("list", `{"n":1}`))
	failed := step("create", `{"id":"t-1"}`)
	failed.Status, failed.Error = runner.StatusError, "POST http://x: connection refused"
	failed.Response = nil
	rec := recOf(failed)
	rec.Status = runner.StatusError

	text := diff.Compare(spot, rec).Text()
	if strings.Contains(text, "length") {
		t.Fatalf("a run that stopped at its first red did not change the chain's length:\n%s", text)
	}
	if !strings.Contains(text, "[fetch..list] not_reached 2 step(s)") {
		t.Fatalf("every step past the stop is not reached:\n%s", text)
	}
	if !strings.Contains(text, "connection refused") || strings.Contains(text, "type ") {
		t.Fatalf("a step that errored sending nothing shows its error, not a type change:\n%s", text)
	}
}

func TestRunDiffCountsAnErroredStepThatSentNothingAsNotReachedAndShowsItsError(t *testing.T) {
	a := runOf("run-a", stepAs("create", runner.StatusPassed, `{"id":"t-1"}`))
	b := runOf("run-b", &runner.StepRecord{ID: "create", Call: "ThingService/Fetch", Status: runner.StatusError,
		Error: "POST http://127.0.0.1:1/x: dial tcp: connection refused"})
	rep := diff.CompareRuns(a, b)
	if len(rep.NoLongerReached) != 1 || len(rep.Changes) != 0 {
		t.Fatalf("an errored step that sent nothing is not reached and has no response to compare, got %s", rep.Text())
	}
	if !strings.Contains(rep.Text(), "connection refused") {
		t.Fatalf("the error text of the step must be shown:\n%s", rep.Text())
	}
}

func TestRunDiffDoesNotBlameKeepGoingWhenTheOtherRunHadNoRedStep(t *testing.T) {
	a := runOf("run-a", stepAs("create", runner.StatusPassed, `{"id":"t-1"}`))
	b := runOf("run-b", stepAs("create", runner.StatusPassed, `{"id":"t-1"}`))
	b.KeepGoing = true
	if text := diff.CompareRuns(a, b).Text(); strings.Contains(text, "past the first red") || strings.Contains(text, "first red") {
		t.Fatalf("run A had no red step, so -keep-going reached nothing extra:\n%s", text)
	}
	c := runOf("run-c", stepAs("create", runner.StatusFailed, `{"id":"t-1"}`))
	if text := diff.CompareRuns(c, b).Text(); !strings.Contains(text, "first red") {
		t.Fatalf("with a red step in the run without -keep-going, the note applies:\n%s", text)
	}
}
