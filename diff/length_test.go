package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestAListWhoseLengthChangedIsReportedAsLengthNotOrder(t *testing.T) {
	long := `{"products":[{"n":1},{"n":2},{"n":3}]}`
	short := `{"products":[{"n":1}]}`
	rep := diff.Compare(spotOf(nil, step("list_all_products", long)), recOf(step("list_all_products", short)))
	if !strings.Contains(rep.Text(), "[list_all_products] length     products want=3 item(s) got=1 item(s)") {
		t.Fatalf("a length change must say length, not order:\n%s", rep.Text())
	}
	runs := diff.CompareRuns(runOf("run-a", stepAs("list_all_products", runner.StatusPassed, long)),
		runOf("run-b", stepAs("list_all_products", runner.StatusPassed, short)))
	if !strings.Contains(runs.Text(), "[list_all_products] length     products a=3 item(s) b=1 item(s)") {
		t.Fatalf("a length change between runs must say length, not order:\n%s", runs.Text())
	}
}
