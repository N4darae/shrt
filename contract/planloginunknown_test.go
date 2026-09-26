package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func loginOptions() contract.PlanOptions {
	return contract.PlanOptions{
		Auth:        true,
		Logins:      []string{"shop.auth.v1.AuthService/Login"},
		LoginBodies: map[string]map[string]any{"shop.auth.v1.AuthService/Login": {"username": "${env.API_USER}", "password": "${env.API_PASSWORD}"}},
	}
}

func loginPlanWhen(t *testing.T, when string) (*contract.Plan, string) {
	t.Helper()
	return shopDemoMutated(t, loginOptions(), func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.auth.v1.AuthService/Login"]; c != nil {
			for i := range c.Failures {
				c.Failures[i].When = when
			}
		}
	}, "Login")
}

func TestAnUnknownUserIsProbedForTheWaysAContractSaysNoAccountHasTheName(t *testing.T) {
	for _, when := range []string{
		"the password is wrong, or no account has this username",
		"the username does not exist or the password does not match it",
		"no such user, or a wrong password",
		"the password is wrong for the account, or the username is an unknown account",
	} {
		p, text := loginPlanWhen(t, when)
		probe := planStep(t, p, "login_unknown_user")
		if bodyAt(t, probe, "username") != "no-such-user-shrt" {
			t.Fatalf("when %q: the probe sends an account name no one has:\n%s", when, text)
		}
		wantExpect(t, probe, "status.details.0.reason", "BadCredentials")
	}
}

func TestAWhenThatSaysNothingOfAnUnknownAccountIsNamedInANote(t *testing.T) {
	p, text := loginPlanWhen(t, "the password does not match the account")
	if _, ok := p.Chain.Step("login_unknown_user"); ok {
		t.Fatalf("nothing says what an unknown account gets:\n%s", text)
	}
	notes := strings.Join(p.Notes, "\n")
	if !strings.Contains(notes, `("the password does not match the account")`) || !strings.Contains(notes, "login_unknown_user") {
		t.Fatalf("the note names the when: it could not read as an unknown account:\n%s", notes)
	}
}
