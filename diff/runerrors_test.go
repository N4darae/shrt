package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestRunDiffComparesTheErrorTextOfAStepBothRunsErroredOn(t *testing.T) {
	a := runOf("run-a", &runner.StepRecord{ID: "create", Call: "ThingService/Create", Status: runner.StatusError,
		Error: "auth login response has no token at \"access_token\""})
	b := runOf("run-b", &runner.StepRecord{ID: "create", Call: "ThingService/Create", Status: runner.StatusError,
		Error: "auth login: POST http://127.0.0.1:1/Login: dial tcp: connection refused"})
	rep := diff.CompareRuns(a, b)
	if rep.Same() {
		t.Fatalf("one run could not log in and the other could not connect: they differ\n%s", rep.Text())
	}
	text := rep.Text()
	for _, want := range []string{"create", "no token at", "connection refused"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the diff must show both error texts, lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "no differences") {
		t.Fatalf("must not claim no differences:\n%s", text)
	}
}

func TestRunDiffKeepsTheSameErrorTextTheSame(t *testing.T) {
	a := runOf("run-a", &runner.StepRecord{ID: "create", Status: runner.StatusError, Error: "dial tcp: connection refused"})
	b := runOf("run-b", &runner.StepRecord{ID: "create", Status: runner.StatusError, Error: "dial tcp: connection refused"})
	if rep := diff.CompareRuns(a, b); !rep.Same() {
		t.Fatalf("the same error twice is no difference:\n%s", rep.Text())
	}
}

func TestRunDiffDoesNotCallAStepSkippedUnderKeepGoingReached(t *testing.T) {
	a := &runner.Record{RunID: "a", Chain: "c", Status: runner.StatusFailed, KeepGoing: true, Steps: []*runner.StepRecord{
		{ID: "create_product", Status: runner.StatusFailed},
		{ID: "add_stock", Status: runner.StatusSkipped, Error: `not sent: ${create_product.product.id_product} reads step "create_product"`},
		{ID: "create_customer", Status: runner.StatusPassed},
	}}
	b := &runner.Record{RunID: "b", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		{ID: "create_product", Status: runner.StatusFailed},
	}}
	text := diff.CompareRuns(a, b).Text()
	if strings.Contains(text, "are reached in A only") {
		t.Fatalf("add_stock was skipped in A, so not every step past the red is reached in A:\n%s", text)
	}
	if !strings.Contains(text, "run A used -keep-going and run B did not") || !strings.Contains(text, "create_customer") {
		t.Fatalf("the note must still name the side and what it reached:\n%s", text)
	}
	if !strings.Contains(text, "skipped in A as well") || !strings.Contains(text, "add_stock") {
		t.Fatalf("the note must name the step A skipped:\n%s", text)
	}
}
