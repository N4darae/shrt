package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestRunDiffNamesTheStepsOnlyOneRunReachedOnce(t *testing.T) {
	a := &runner.Record{RunID: "a", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		{ID: "create_product", Status: runner.StatusFailed},
	}}
	b := &runner.Record{RunID: "b", Chain: "c", Status: runner.StatusFailed, KeepGoing: true, Steps: []*runner.StepRecord{
		{ID: "create_product", Status: runner.StatusFailed},
		{ID: "add_stock", Status: runner.StatusPassed},
		{ID: "create_customer", Status: runner.StatusPassed},
	}}
	text := diff.CompareRuns(a, b).Text()
	if n := strings.Count(text, "add_stock, create_customer"); n != 1 {
		t.Fatalf("the steps only B reached are listed %d times, want once:\n%s", n, text)
	}
	if !strings.Contains(text, "run B used -keep-going and run A did not") {
		t.Fatalf("the keep-going note must stay:\n%s", text)
	}
	if !strings.Contains(text, "reached in B, not reached in A: add_stock, create_customer") {
		t.Fatalf("the reached line must stay:\n%s", text)
	}
}
