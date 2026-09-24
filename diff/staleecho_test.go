package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func staleSteps(sent, echoed, message string) []*runner.StepRecord {
	return []*runner.StepRecord{
		{ID: "cust", Call: "S/CreateCustomer", Status: runner.StatusPassed,
			Request:  []byte(`{"email":"` + sent + `"}`),
			Response: []byte(`{"customer":{"email":"` + echoed + `"},"status":{"message":"` + message + `"}}`)},
	}
}

func TestAResponseStillCarryingTheConfirmedFixtureNameIsAChange(t *testing.T) {
	fixture := func(step, path string) bool { return step == "cust" && path == "email" }
	for _, tc := range []struct {
		name, echoed, message string
		changed               []string
	}{
		{"echoes the new name", "w-px3@example.test", "made w-px3@example.test", nil},
		{"echoes the old email", "w-g1@example.test", "made w-px3@example.test", []string{"cust customer.email"}},
		{"old name in a message", "w-px3@example.test", "made w-g1@example.test", []string{"cust status.message"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: staleSteps("w-g1@example.test", "w-g1@example.test", "made w-g1@example.test")}
			rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: staleSteps("w-px3@example.test", tc.echoed, tc.message)}
			rep := diff.CompareMasking(spot, rec, nil)
			rep.RequestChanges = diff.CompareRequests(spot, rec, nil)
			rep.SeparateInput(spot, rec, nil, fixture)
			got := []string{}
			for _, c := range rep.Changes {
				got = append(got, c.Step+" "+c.Path)
			}
			if strings.Join(got, ",") != strings.Join(tc.changed, ",") {
				t.Fatalf("verify changes %v, want %v\n%s", got, tc.changed, rep.Text())
			}
			if len(tc.changed) > 0 && !strings.Contains(rep.Text(), "want=w-px3@example.test") && !strings.Contains(rep.Text(), "want=made w-px3@example.test") {
				t.Fatalf("the change says what an echo of this run's input would read:\n%s", rep.Text())
			}
			a := &runner.Record{RunID: "a", Chain: "c", Status: runner.StatusPassed, Steps: spot.Steps}
			runs := diff.CompareRunsSkipping(a, rec, nil, fixture)
			got = got[:0]
			for _, c := range runs.Changes {
				got = append(got, c.Step+" "+c.Path)
			}
			if strings.Join(got, ",") != strings.Join(tc.changed, ",") {
				t.Fatalf("diff changes %v, want %v\n%s", got, tc.changed, runs.Text())
			}
		})
	}
}
