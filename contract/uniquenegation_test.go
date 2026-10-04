package contract_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func TestPlanDoesNotPadAValueTheContractSaysIsNotTrimmed(t *testing.T) {
	for _, note := range []string{
		"whitespace only is invalid_argument; compared byte for byte with no trimming",
		"compared as sent, not trimmed",
		"compared without trimming surrounding whitespace",
		"untrimmed; a blank or whitespace-only email is invalid_argument",
	} {
		text, _ := planYAML(t, customerOverlay(note, "another customer already has this email"))
		if strings.Contains(text, "_space") {
			t.Fatalf("note %q says the value is not trimmed, so no padded variant may be asserted:\n%s", note, text)
		}
	}
}

func TestPlanDoesNotSendACaseVariantWhenCaseIsSaidToMatter(t *testing.T) {
	for _, when := range []string{
		"another customer already has this email; case-sensitive, not ignoring case",
		"another customer already has this email, compared case sensitively",
		"another customer already has this email (the comparison does not ignore case)",
	} {
		text, _ := planYAML(t, customerOverlay("must be unused", when))
		if strings.Contains(text, "_case") {
			t.Fatalf("when %q says case matters, so no case variant may be asserted:\n%s", when, text)
		}
	}
}

func TestPlanStillPadsWhenTheContractSaysTrimmed(t *testing.T) {
	text, _ := planYAML(t, customerOverlay("surrounding whitespace is stripped before the comparison", "another customer already has this email"))
	if !strings.Contains(text, "_space") {
		t.Fatalf("stripped surrounding whitespace means a padded value is the same value:\n%s", text)
	}
}

func uniqueOverlay(unique string) string {
	return `apiVersion: shrt/contract/v1
domain: customers
rpcs:
    shop.customers.v1.CustomerService/CreateCustomer:
        summary: registers a customer
        required: [email]
        fields:
            email:
                value: cust-${uuid}@example.test
                note: whitespace only is invalid_argument
        failures:
            - code: 1101
              reason: EmailTaken
              when: another customer already has this email, ignoring case
              unique: ` + unique + `
        status: draft
`
}

func TestStructuredUniqueComparisonWinsOverTheNotes(t *testing.T) {
	text, _ := planYAML(t, uniqueOverlay("{case: exact, trim: true}"))
	if strings.Contains(text, "_case") || !strings.Contains(text, "_space") {
		t.Fatalf("unique: {case: exact, trim: true} means no case variant and a padded one, whatever the prose says:\n%s", text)
	}
	text, _ = planYAML(t, uniqueOverlay("{case: ignore}"))
	if !strings.Contains(text, "_case") || strings.Contains(text, "_space") {
		t.Fatalf("unique: {case: ignore} means a case variant and, trim unset and prose silent, no padded one:\n%s", text)
	}
}

func TestContractLintRefusesAnUnknownUniqueCase(t *testing.T) {
	issues := contract.LintLibrary(libraryFrom(t, uniqueOverlay("{case: lower}")), catalogtest.ShopWithErrorDetails())
	if !slices.ContainsFunc(issues, func(i contract.Issue) bool { return i.IsError() && strings.Contains(i.Message, "unique.case") }) {
		t.Fatalf("unique.case must be ignore or exact: %v", issues)
	}
}
