package contract_test

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func literalLength(s string) int {
	for {
		i := strings.Index(s, "${")
		if i < 0 {
			return utf8.RuneCountInString(s)
		}
		j := strings.Index(s[i:], "}")
		if j < 0 {
			return utf8.RuneCountInString(s)
		}
		s = s[:i] + s[i+j+1:]
	}
}

func TestPlanSendsLongAndMultiByteTextAndAssertsItEchoedAndStored(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateCustomer")
	long := planStep(t, p, "create_customer_long_text")
	name := bodyAt(t, long, "name")
	if literalLength(name) < 64 {
		t.Fatalf("the long name has at least 64 characters besides its vars, got %q:\n%s", name, text)
	}
	email := bodyAt(t, long, "email")
	if !strings.HasSuffix(email, "@example.test") || literalLength(email) < 64 {
		t.Fatalf("the long email keeps its domain and grows before the @, got %q:\n%s", email, text)
	}
	wantExpect(t, long, "customer.name", "${steps.create_customer_long_text.request.name}")
	wantExpect(t, long, "customer.email", "${steps.create_customer_long_text.request.email}")
	read := planStep(t, p, "get_customer_after_create_customer_long_text")
	if got := bodyAt(t, read, "id_customer"); got != "${create_customer_long_text.customer.id_customer}" {
		t.Fatalf("the read-back reads the long customer, got %s:\n%s", got, text)
	}
	wantExpect(t, read, "customer.name", "${steps.create_customer_long_text.request.name}")

	uni := planStep(t, p, "create_customer_unicode_text")
	if n := bodyAt(t, uni, "name"); utf8.RuneCountInString(n) == len(n) {
		t.Fatalf("the unicode name has multi-byte characters, got %q:\n%s", n, text)
	}
	if strings.ContainsFunc(bodyAt(t, uni, "email"), func(r rune) bool { return r > 127 }) {
		t.Fatalf("an email is not sent multi-byte:\n%s", text)
	}
	wantExpect(t, planStep(t, p, "get_customer_after_create_customer_unicode_text"), "customer.name", "${steps.create_customer_unicode_text.request.name}")
	if !strings.Contains(notes, "create_customer_long_text") {
		t.Fatalf("a note names the long-text probe: %s", notes)
	}
}

func TestPlanProbesAStatedMaximumLengthAtAndOverIt(t *testing.T) {
	p := editedPlan(t, func(name, body string) string {
		if name != "customers.yaml" {
			return body
		}
		body = strings.Replace(body, "note: free display name, not validated by the handler",
			"note: free display name, at most 40 characters", 1)
		return strings.Replace(body, "      when: the email does not contain an @ character\n",
			"      when: the email does not contain an @ character\n            - connect_code: invalid_argument\n              reason: NameTooLong\n              field: name\n              when: name is longer than 40 characters\n", 1)
	}, "CreateCustomer")
	raw, _ := p.YAML()
	at := planStep(t, p, "create_customer_name_at_max")
	if n := literalLength(bodyAt(t, at, "name")); n != 40 || strings.Contains(bodyAt(t, at, "name"), "${") {
		t.Fatalf("the name at its maximum is 40 characters, got %d:\n%s", n, raw)
	}
	wantExpect(t, at, "customer.name", "${steps.create_customer_name_at_max.request.name}")
	over := planStep(t, p, "create_customer_name_over_max")
	if n := literalLength(bodyAt(t, over, "name")); n != 41 {
		t.Fatalf("the name over its maximum is 41 characters, got %d:\n%s", n, raw)
	}
	wantExpect(t, over, "transport.code", "invalid_argument")
	long := planStep(t, p, "create_customer_long_text")
	if n := literalLength(bodyAt(t, long, "name")); n > 40 {
		t.Fatalf("the long-text probe stays within the stated maximum, got %d:\n%s", n, raw)
	}
}
