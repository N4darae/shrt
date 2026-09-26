package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestRunDiffNamesAHeaderAndFoldsAFixtureReference(t *testing.T) {
	run := func(id, tag string, headers map[string]string) *runner.Record {
		return runOf(id,
			&runner.StepRecord{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Headers: map[string]string{},
				Request: json.RawMessage(`{"sku":"sku-` + tag + `-a"}`), Response: json.RawMessage(`{"ok":true}`)},
			&runner.StepRecord{ID: "dup", Call: "S/Create", Status: runner.StatusPassed, Headers: headers,
				Request: json.RawMessage(`{"sku":"sku-` + tag + `-a"}`), Response: json.RawMessage(`{"ok":false}`)})
	}
	a := run("a", "t1", map[string]string{})
	b := run("b", "t2", map[string]string{"X-Trace-Note": "lab"})
	fx := diff.Fixtures{Named: func(step, path string) bool { return step == "create" && path == "sku" }}
	rep := diff.CompareRunsSkipping(a, b, nil, fx)
	text := rep.Text()
	if !strings.Contains(text, "[dup] unexpected headers.X-Trace-Note") {
		t.Fatalf("a header sent in one run only is a request difference:\n%s", text)
	}
	if strings.Contains(text, "[dup] changed    sku") {
		t.Fatalf("a reference that differs only by the fixture name it copies is not a changed request value:\n%s", text)
	}
}
