package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
)

func TestDiffDoesNotCallFixtureVarsADifferentInput(t *testing.T) {
	named := func(step, path string) bool { return step == "create" && path == "sku" }
	a, b := fixtureRun("a", "first", "ok"), fixtureRun("b", "second", "ok")
	a.Vars, b.Vars = map[string]any{"tag": "first"}, map[string]any{"tag": "second"}
	fx := diff.Fixtures{Named: named, Var: func(name string) bool { return name == "tag" }}
	text := diff.CompareRunsSkipping(a, b, nil, fx).Text()
	if strings.Contains(text, "the runs used different vars") {
		t.Fatalf("tag is only a fixture var whose echoes are masked; it is no different input:\n%s", text)
	}
	if !strings.Contains(text, "fixture vars differ, echoes masked: tag a=first b=second") {
		t.Fatalf("the report still names the fixture var:\n%s", text)
	}
	a.Vars["qty"], b.Vars["qty"] = 1, 2
	text = diff.CompareRunsSkipping(a, b, nil, fx).Text()
	if !strings.Contains(text, "the runs used different vars, so a difference may come from the input rather than the backend: qty a=1 b=2") {
		t.Fatalf("a var that is not a fixture var is still a different input:\n%s", text)
	}
	if !strings.Contains(text, "fixture vars differ, echoes masked: tag a=first b=second") {
		t.Fatalf("the fixture var is named apart:\n%s", text)
	}
}
