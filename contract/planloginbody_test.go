package contract_test

import (
	"strings"
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

func TestPlanForTheLoginSendsThePasswordPaddedAndExpectsItRefused(t *testing.T) {
	opts := contract.PlanOptions{
		Auth:   true,
		Logins: []string{"shop.auth.v1.AuthService/Login"},
		LoginBodies: map[string]map[string]any{
			"shop.auth.v1.AuthService/Login": {"username": "${env.API_USER}", "password": "${env.API_PASSWORD}"},
		},
		Redact: []string{"**.*password"},
	}
	p, text, notes := shopDemoPlanWith(t, opts, "Login")
	padded := planStep(t, p, "login_padded_password")
	if got := bodyAt(t, padded, "password"); got != " ${env.API_PASSWORD} " {
		t.Fatalf("the secret gets a space on each side, got %q:\n%s", got, text)
	}
	if got := bodyAt(t, padded, "username"); got != "${env.API_USER}" {
		t.Fatalf("the username is sent as it is, got %q:\n%s", got, text)
	}
	bad := planStep(t, p, "login_bad_password")
	if len(padded.Expect) != len(bad.Expect) {
		t.Fatalf("the padded secret is refused as a wrong one is: %+v vs %+v", padded.Expect, bad.Expect)
	}
	for i := range bad.Expect {
		if padded.Expect[i].Path != bad.Expect[i].Path {
			t.Fatalf("the padded secret is refused as a wrong one is: %+v vs %+v", padded.Expect, bad.Expect)
		}
	}
	if !strings.Contains(notes, "login_padded_password sends password with a leading and a trailing space") {
		t.Fatalf("the login note names the probe:\n%s", notes)
	}
}
