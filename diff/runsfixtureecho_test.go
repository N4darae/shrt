package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func fixtureRun(id, tag, message string) *runner.Record {
	return &runner.Record{RunID: id, Chain: "c", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed,
			Request:  []byte(`{"sku":"sku-` + tag + `"}`),
			Response: []byte(`{"product":{"sku":"sku-` + tag + `","name":"Widget sku-` + tag + `"},"note":"` + message + `"}`)},
	}}
}

func TestDiffMasksAResponseValueThatOnlyEchoesTheFixtureName(t *testing.T) {
	fixture := func(step, path string) bool { return step == "create" && path == "sku" }
	rep := diff.CompareRunsSkipping(fixtureRun("a", "first", "ok"), fixtureRun("b", "second", "ok"), nil, diff.Fixtures{Named: fixture})
	if len(rep.Changes) != 0 {
		t.Fatalf("both response values only echo the new sku, as verify masks them; want no change, got %d:\n%s", len(rep.Changes), rep.Text())
	}
	if !strings.Contains(rep.Text(), "not counted: 3 (-masked lists them)") {
		t.Fatalf("the report counts the echoes:\n%s", rep.Text())
	}
	rep = diff.CompareRunsSkipping(fixtureRun("a", "first", "ok"), fixtureRun("b", "second", "late"), nil, diff.Fixtures{Named: fixture})
	if len(rep.Changes) != 1 || rep.Changes[0].Path != "note" {
		t.Fatalf("a real response difference is still shown: %+v\n%s", rep.Changes, rep.Text())
	}
}
