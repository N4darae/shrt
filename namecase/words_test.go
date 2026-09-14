package namecase_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/namecase"
)

func TestWordsSplitsEveryConventionARepoMightUse(t *testing.T) {
	cases := map[string]string{
		"id_customer":    "id customer",
		"customer_id":    "customer id",
		"customerId":     "customer id",
		"CustomerID":     "customer id",
		"IDCustomer":     "id customer",
		"id":             "id",
		"uuid":           "uuid",
		"customer_uuid":  "customer uuid",
		"identity":       "identity",
		"idempotencyKey": "idempotency key",
		"amount_minor":   "amount minor",
		"http2Port":      "http2 port",
		"orderIDs":       "order ids",
		"customerIDs":    "customer ids",
		"IDs":            "ids",
		"customer_ids":   "customer ids",
		"HTTPServer":     "http server",
		"a":              "a",
		"":               "",
	}
	for in, want := range cases {
		got := strings.Join(namecase.Words(in), " ")
		if got != want {
			t.Errorf("Words(%q) = %q, want %q", in, got, want)
		}
	}
}
