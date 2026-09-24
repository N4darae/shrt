package diff_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func orderSpot(body string) *store.SafeSpot {
	return &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "fetch_order", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(body)},
	}}
}

func TestIdNamedFieldIsMaskedOnlyWhenBothSidesAreIdShaped(t *testing.T) {
	want := `{"order":{"id_order":"ord-819dac5f23ba","id_customer":"cus-5656cb156e31","created_at":"2026-09-24T15:59:18.1Z","total":"500"}}`
	fresh := `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210","created_at":"2026-09-25T10:00:00Z","total":"500"}}`
	rep := diff.Compare(orderSpot(want), runOf("run", stepAs("fetch_order", runner.StatusPassed, fresh)))
	if !rep.Clean() || rep.Masked != 3 {
		t.Fatalf("fresh ids and timestamps differ every run and must be masked, masked=%d:\n%s", rep.Masked, rep.Text())
	}
	for name, got := range map[string]string{
		"empty id":        `{"order":{"id_order":"ord-0123456789ab","id_customer":"","created_at":"2026-09-25T10:00:00Z","total":"500"}}`,
		"undefined id":    `{"order":{"id_order":"ord-0123456789ab","id_customer":"undefined","created_at":"2026-09-25T10:00:00Z","total":"500"}}`,
		"null id":         `{"order":{"id_order":"ord-0123456789ab","id_customer":null,"created_at":"2026-09-25T10:00:00Z","total":"500"}}`,
		"number id":       `{"order":{"id_order":"ord-0123456789ab","id_customer":7,"created_at":"2026-09-25T10:00:00Z","total":"500"}}`,
		"missing id":      `{"order":{"id_customer":"cus-ba9876543210","created_at":"2026-09-25T10:00:00Z","total":"500"}}`,
		"empty timestamp": `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210","created_at":"","total":"500"}}`,
		"junk timestamp":  `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210","created_at":"never","total":"500"}}`,
	} {
		rep := diff.Compare(orderSpot(want), runOf("run", stepAs("fetch_order", runner.StatusPassed, got)))
		if rep.Clean() {
			t.Errorf("%s: a change to a value that is not id- or timestamp-shaped is drift, got no drift:\n%s", name, rep.Text())
		}
		runs := diff.CompareRuns(runOf("a", stepAs("fetch_order", runner.StatusPassed, want)), runOf("b", stepAs("fetch_order", runner.StatusPassed, got)))
		if runs.Same() {
			t.Errorf("%s: shrt diff must show it too:\n%s", name, runs.Text())
		}
	}
}

func TestNumericIdsOnBothSidesAreMasked(t *testing.T) {
	rep := diff.Compare(orderSpot(`{"order":{"id":41}}`), runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"id":42}}`)))
	if !rep.Clean() || rep.Masked != 1 {
		t.Fatalf("two numeric ids are id-shaped, masked=%d:\n%s", rep.Masked, rep.Text())
	}
	rep = diff.Compare(orderSpot(`{"order":{"id":41}}`), runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"id":0}}`)))
	if rep.Clean() {
		t.Fatalf("an id that became 0 is drift:\n%s", rep.Text())
	}
}
