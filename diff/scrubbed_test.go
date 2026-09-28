package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestAValueScrubbedForHoldingASecretIsNotSaidToBeUnderARedactPath(t *testing.T) {
	body := `{"customer":{"name":"<redacted>","token":"<redacted>"}}`
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, body))
	rec.Redacted = []string{"**.token", "**.*password"}
	text := diff.Compare(orderSpot(body), rec).MaskedList()
	if !strings.Contains(text, "under redact paths, blanked in the records, not compared:\n  fetch_order customer.token") {
		t.Fatalf("customer.token is under a redact path:\n%s", text)
	}
	if !strings.Contains(text, "scrubbed by value for holding a secret the run sent, not compared:\n  fetch_order customer.name") {
		t.Fatalf("customer.name is under no redact path; it was blanked because its value held a secret, and must be named so:\n%s", text)
	}
}
