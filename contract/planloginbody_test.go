package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestPlanForTheLoginSendsTheAuthBlocksCredentialsNotEmptyStrings(t *testing.T) {
	opts := contract.PlanOptions{
		Auth:   true,
		Logins: []string{"shop.auth.v1.AuthService/Login"},
		LoginBodies: map[string]map[string]any{
			"shop.auth.v1.AuthService/Login": {"username": "${env.API_USER}", "password": "${env.API_PASSWORD}"},
		},
	}
	p, text, _ := shopDemoPlanWith(t, opts, "Login")
	login := planStep(t, p, "login")
	if got := bodyAt(t, login, "username"); got != "${env.API_USER}" {
		t.Fatalf("username comes from the auth block, got %q:\n%s", got, text)
	}
	if got := bodyAt(t, login, "password"); got != "${env.API_PASSWORD}" {
		t.Fatalf("password comes from the auth block, got %q:\n%s", got, text)
	}
}
