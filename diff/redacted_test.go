package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestRedactedValuesAreCountedAsNeverCompared(t *testing.T) {
	body := `{"product":{"qty_on_hand":"<redacted>","name":"Widget"}}`
	rep := diff.Compare(orderSpot(body), runOf("run", stepAs("fetch_order", runner.StatusPassed, body)))
	if rep.Redacted != 1 || len(rep.RedactedPaths) != 1 || rep.RedactedPaths[0] != "fetch_order product.qty_on_hand" {
		t.Fatalf("a redacted value is blanked on both sides, so it is never compared and must be counted: %+v", rep)
	}
	if !strings.Contains(rep.Text(), "not counted: 1") || !strings.Contains(rep.MaskedList(), "redact paths, blanked in the records, not compared:\n  fetch_order product.qty_on_hand") {
		t.Fatalf("the report must say which redacted values were not compared:\n%s\n%s", rep.Text(), rep.MaskedList())
	}
}
