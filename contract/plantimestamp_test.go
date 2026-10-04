package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func TestPlanAssertsALoginTokensExpiry(t *testing.T) {
	for _, tc := range []struct{ name, summary, terminal, want, why string }{
		{"against the lifetime the contract states", "Exchange a username and password for a bearer token valid for one hour.",
			"        terminal:\n            expires_at: unix seconds when the token stops working\n",
			"- path: expires_at\n          within:\n            of: ${nowunix+3600}\n            by: 5\n",
			"the contract says the token is valid for one hour, so the plan asserts expires_at within 5s of ${nowunix+3600}"},
		{"in the future when no lifetime is stated", "Exchange a username and password for a bearer token.", "",
			"- path: expires_at\n          gte: ${nowunix}\n",
			"with no lifetime stated the plan still asserts the token is not already expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lib := libraryFrom(t, `apiVersion: shrt/contract/v1
domain: auth
rpcs:
    shrt.test.v1.AuthService/Login:
        summary: `+tc.summary+`
        required: [username, password]
        fields:
            username:
                value: u
            password:
                value: p
`+tc.terminal+`        status: draft
`)
			p, err := contract.BuildPlan("shrt.test.v1.AuthService/Login", lib, catalogtest.New(), "login")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := p.YAML()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), tc.want) {
				t.Fatalf("%s:\n%s", tc.why, raw)
			}
		})
	}
}
