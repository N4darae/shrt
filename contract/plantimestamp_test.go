package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func TestPlanAssertsAnExpiryAgainstTheLifetimeTheContractStates(t *testing.T) {
	lib := libraryFrom(t, `apiVersion: shrt/contract/v1
domain: auth
rpcs:
    shrt.test.v1.AuthService/Login:
        summary: Exchange a username and password for a bearer token valid for one hour.
        required: [username, password]
        fields:
            username:
                value: u
            password:
                value: p
        terminal:
            expires_at: unix seconds when the token stops working
        status: draft
`)
	p, err := contract.BuildPlan("shrt.test.v1.AuthService/Login", lib, catalogtest.New(), "login")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.YAML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "- path: expires_at\n          within:\n            of: ${nowunix+3600}\n            by: 5\n") {
		t.Fatalf("the contract says the token is valid for one hour, so the plan asserts expires_at within 5s of ${nowunix+3600}:\n%s", raw)
	}
}

func TestPlanAssertsAnExpiryIsInTheFutureWhenNoLifetimeIsStated(t *testing.T) {
	lib := libraryFrom(t, `apiVersion: shrt/contract/v1
domain: auth
rpcs:
    shrt.test.v1.AuthService/Login:
        summary: Exchange a username and password for a bearer token.
        required: [username, password]
        fields:
            username:
                value: u
            password:
                value: p
        status: draft
`)
	p, err := contract.BuildPlan("shrt.test.v1.AuthService/Login", lib, catalogtest.New(), "login")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := p.YAML()
	if !strings.Contains(string(raw), "- path: expires_at\n          gte: ${nowunix}\n") {
		t.Fatalf("with no lifetime stated the plan still asserts the token is not already expired:\n%s", raw)
	}
}
