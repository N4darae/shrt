package diff_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func step(id, body string) *runner.StepRecord {
	return &runner.StepRecord{
		Index: 1, ID: id, Call: "ThingService/Fetch", Status: runner.StatusPassed,
		Response: json.RawMessage(body),
	}
}

func spotOf(volatile []string, steps ...*runner.StepRecord) *store.SafeSpot {
	return &store.SafeSpot{Chain: "thing-flow", RunID: "safe-1", Volatile: volatile, Steps: steps}
}

func recOf(steps ...*runner.StepRecord) *runner.Record {
	return &runner.Record{Chain: "thing-flow", RunID: "run-9", Status: runner.StatusPassed, Steps: steps}
}

func TestIdenticalRunsHaveNoDrift(t *testing.T) {
	body := `{"id":"thing-1","name":"widget"}`
	rep := diff.Compare(spotOf(nil, step("fetch", body)), recOf(step("fetch", body)))
	if !rep.Clean() {
		t.Fatalf("want no drift, got %s", rep.Text())
	}
}

func TestChangedFieldIsReportedWithBothValues(t *testing.T) {
	rep := diff.Compare(
		spotOf(nil, step("fetch", `{"id":"thing-1","name":"widget"}`)),
		recOf(step("fetch", `{"id":"thing-1","name":"gadget"}`)),
	)
	if len(rep.Changes) != 1 {
		t.Fatalf("want 1 change, got %s", rep.Text())
	}
	c := rep.Changes[0]
	if c.Step != "fetch" || c.Path != "name" || c.Kind != diff.KindChanged {
		t.Fatalf("unexpected change %+v", c)
	}
	if c.Want != "widget" || c.Got != "gadget" {
		t.Fatalf("want widget->gadget, got %v->%v", c.Want, c.Got)
	}
}

func TestVolatilePathsAreMaskedBeforeComparing(t *testing.T) {
	rep := diff.Compare(
		spotOf([]string{"**.created_at"}, step("fetch", `{"id":"a","created_at":"t1"}`)),
		recOf(step("fetch", `{"id":"a","created_at":"t2"}`)),
	)
	if !rep.Clean() {
		t.Fatalf("a volatile field must not count as drift, got %s", rep.Text())
	}
}

func TestMissingAndUnexpectedFieldsAreDistinguished(t *testing.T) {
	rep := diff.Compare(
		spotOf(nil, step("fetch", `{"id":"a","name":"widget"}`)),
		recOf(step("fetch", `{"id":"a","label":"widget"}`)),
	)
	kinds := map[string]string{}
	for _, c := range rep.Changes {
		kinds[c.Path] = c.Kind
	}
	if kinds["name"] != diff.KindMissing {
		t.Fatalf("want name missing, got %v", kinds)
	}
	if kinds["label"] != diff.KindUnexpected {
		t.Fatalf("want label unexpected, got %v", kinds)
	}
}

func TestStepReorderIsReportedRatherThanDiffed(t *testing.T) {
	a, b := step("create", `{"id":"a"}`), step("fetch", `{"id":"a"}`)
	rep := diff.Compare(spotOf(nil, a, b), recOf(b, a))
	if rep.Clean() {
		t.Fatal("reordering steps must be reported")
	}
	for _, c := range rep.Changes {
		if c.Kind == diff.KindOrder {
			return
		}
	}
	t.Fatalf("want an order change, got %s", rep.Text())
}

func TestMaskPatterns(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"**.created_at", "created_at", true},
		{"**.created_at", "deals.0.created_at", true},
		{"deals.*.id", "deals.3.id", true},
		{"deals.*.id", "deals.3.4.id", false},
		{"error.code", "error.code", true},
		{"error.code", "error.message", false},
	}
	for _, c := range cases {
		if got := pathmask.Match(c.pattern, c.path); got != c.want {
			t.Fatalf("Match(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}
