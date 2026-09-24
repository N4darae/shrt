package store

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestAlsoBaselinedLeavesOutAStepsVolatileListAndCountsAnUnorderedOne(t *testing.T) {
	body := []byte(`{"status":{"code":"OK"},"products":[{"name":"a","price_minor":100},{"name":"b","price_minor":200}],"total":2}`)
	rec := &runner.Record{}
	masked := alsoBaselined(rec, &runner.StepRecord{ID: "l", Response: body, Volatile: []string{"products"}})
	if strings.Contains(masked, "products") || !strings.Contains(masked, "total=2") {
		t.Errorf("a list under the step's volatile is not baselined, so it is not listed: %q", masked)
	}
	set := alsoBaselined(rec, &runner.StepRecord{ID: "l", Response: body, Unordered: []string{"products"}})
	if strings.Contains(set, "products.0") || !strings.Contains(set, "products=2 item(s) in any order") {
		t.Errorf("an unordered list is compared as a multiset, not by index: %q", set)
	}
}
