package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestANewRedactPatternNamesTheRequestValueItBlankedToo(t *testing.T) {
	step := func(email string) *runner.StepRecord {
		return &runner.StepRecord{ID: "cust", Call: "S/CreateCustomer", Status: runner.StatusPassed,
			Request: []byte(`{"email":"` + email + `"}`), Response: []byte(`{"customer":{"email":"` + email + `"}}`)}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{step("w@example.test")}}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Redacted: []string{"**.email"},
		Steps: []*runner.StepRecord{step("<redacted>")}}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.NoteApprovedRedact(nil, rec)
	rep.NoteRedactedRequests(spot, rec)
	text := rep.Text()
	for _, want := range []string{"cust customer.email", "cust request email"} {
		if !strings.Contains(text, want) {
			t.Errorf("want %q among the values the new pattern blanked:\n%s", want, text)
		}
	}
}
