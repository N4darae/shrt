package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestNullStepCannotProduceACleanDiff(t *testing.T) {
	valid := step("fetch", `{"id":"a"}`)
	for _, tc := range []struct {
		name     string
		was, now []*runner.StepRecord
	}{
		{"safe spot", []*runner.StepRecord{nil, valid}, []*runner.StepRecord{valid}},
		{"run", []*runner.StepRecord{valid}, []*runner.StepRecord{valid, nil}},
		{"both", []*runner.StepRecord{nil}, []*runner.StepRecord{nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spot, rec := spotOf(nil, tc.was...), recOf(tc.now...)
			for name, rep := range map[string]*diff.Report{
				"responses": diff.Compare(spot, rec),
				"requests":  diff.CompareWithRequests(spot, rec, nil, nil),
			} {
				if rep.Clean() || len(rep.Changes) == 0 || !strings.Contains(rep.Changes[0].Detail, "empty") {
					t.Fatalf("%s: want an explicit empty-step diagnostic, got %+v", name, rep)
				}
				if !strings.Contains(rep.Text(), "empty") {
					t.Fatalf("diagnostic is not visible in text: %s", rep.Text())
				}
			}
		})
	}
}
