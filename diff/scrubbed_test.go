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
	text := diff.Compare(orderSpot(body), rec).Text()
	if !strings.Contains(text, "fetch_order customer.token") || !strings.Contains(text, "under redact paths") {
		t.Fatalf("customer.token is under a redact path:\n%s", text)
	}
	line := ""
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "customer.name") {
			line = l
		}
	}
	if !strings.Contains(line, "scrubbed by value") || strings.Contains(line, "under redact paths") {
		t.Fatalf("customer.name is under no redact path; it was blanked because its value held a secret, and must be named so:\n%s", text)
	}
}
