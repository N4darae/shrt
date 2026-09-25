package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func customerOverlay(note, when string) string {
	return `apiVersion: shrt/contract/v1
domain: customers
rpcs:
    shop.customers.v1.CustomerService/CreateCustomer:
        summary: registers a customer
        required: [email]
        fields:
            email:
                value: cust-${uuid}@example.test
                note: ` + note + `
        failures:
            - code: 1101
              reason: EmailTaken
              when: ` + when + `
        status: draft
`
}

func planYAML(t *testing.T, overlay string) (string, string) {
	t.Helper()
	path, ok := chain.EnvelopePath(), chain.EnvelopeOK()
	contract.ApplyConventions(nil, "status.code", "SUCCESS")
	t.Cleanup(func() { contract.ApplyConventions(nil, path, ok) })
	p, err := contract.BuildPlan("shop.customers.v1.CustomerService/CreateCustomer", libraryFrom(t, overlay), catalogtest.ShopWithErrorDetails(), "cust")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.YAML()
	if err != nil {
		t.Fatal(err)
	}
	return string(raw), strings.Join(p.Notes, "\n")
}

func TestPlanForAUniquenessRefusalThatIgnoresCaseTriesTheSameValueInAnotherCase(t *testing.T) {
	text, notes := planYAML(t, customerOverlay("must be unused", "another customer already has this email, ignoring case"))
	for _, want := range []string{
		"email: cust-${vars.tag}@example.test",
		"- id: create_customer_same_email\n",
		"email: ${steps.create_customer.request.email}",
		"- id: create_customer_same_email_case\n",
		"email: CUST-${vars.tag}@EXAMPLE.TEST",
		"- path: status.code\n          not_equal: SUCCESS",
		"- path: status.details.0.reason\n          equals: EmailTaken",
		"- path: status.details.0.app_code\n          equals: 1101",
		"- path: customer\n          exists: false",
		"tag: cust",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("want %q in the plan:\n%s\n%s", want, text, notes)
		}
	}
	if strings.Contains(text, "create_customer_same_email_space") {
		t.Fatalf("the contract says nothing about whitespace, so no whitespace variant is asserted:\n%s", text)
	}
	if !strings.Contains(notes, "ignoring case") && !strings.Contains(notes, "case variant") {
		t.Fatalf("the plan says why it sends a case variant: %s", notes)
	}
}

func TestPlanForAUniquenessRefusalSilentOnCaseSendsOnlyTheExactDuplicate(t *testing.T) {
	text, notes := planYAML(t, customerOverlay("must be unused", "another customer already has this email"))
	if !strings.Contains(text, "- id: create_customer_same_email\n") || !strings.Contains(text, "email: cust-${uuid}@example.test") {
		t.Fatalf("an exact duplicate needs no rewrite of the fresh value:\n%s", text)
	}
	if strings.Contains(text, "_case") {
		t.Fatalf("the contract does not say case is ignored, so a case variant would be a guess:\n%s", text)
	}
	if !strings.Contains(notes, "ignores case") {
		t.Fatalf("the plan says how to get a case variant asserted: %s", notes)
	}
}

func TestPlanForAUniquenessRefusalThatTrimsTriesThePaddedValue(t *testing.T) {
	text, _ := planYAML(t, customerOverlay("compared case-insensitively after trimming surrounding whitespace", "another customer already has this email"))
	for _, want := range []string{
		"- id: create_customer_same_email_case\n",
		"- id: create_customer_same_email_space\n",
		`email: ' ${steps.create_customer.request.email} '`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("want %q in the plan:\n%s", want, text)
		}
	}
}
