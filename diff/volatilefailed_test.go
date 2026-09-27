package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestAVolatileListShowsItsLengthOnlyWhenItsOwnExpectationFailed(t *testing.T) {
	long := `{"products":[{"id":"a"},{"id":"b"},{"id":"c"}]}`
	short := `{"products":[{"id":"z"}]}`
	failed := func() *runner.StepRecord {
		st := stepAs("list_all", runner.StatusFailed, short)
		st.Expect = []chain.ExpectResult{{Path: "products", Rule: "includes", Want: map[string]any{"id": "a"}, Got: 1}}
		return st
	}
	want := "[list_all] length     products want=3 item(s) got=1 item(s) (" + diff.VolatileFailed + ")"
	rep := diff.CompareMasking(spotOf([]string{"products"}, step("list_all", long)), recOf(failed()), nil)
	if !strings.Contains(rep.Text(), want) {
		t.Fatalf("a volatile list whose expectation failed must show its length change:\n%s", rep.Text())
	}
	runs := diff.CompareRunsMasking(runOf("run-a", stepAs("list_all", runner.StatusPassed, long)), runOf("run-b", failed()), []string{"products"})
	if !strings.Contains(runs.Text(), "products a=3 item(s) b=1 item(s) ("+diff.VolatileFailed+")") {
		t.Fatalf("diff must show the length change too:\n%s", runs.Text())
	}
	quiet := diff.CompareMasking(spotOf([]string{"products"}, step("list_all", long)), recOf(step("list_all", short)), nil)
	if !quiet.Clean() {
		t.Fatalf("a volatile list whose expectations passed stays masked:\n%s", quiet.Text())
	}
}
