package runner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func principalOf(t *testing.T, user, password string) (string, string) {
	t.Helper()
	srv := newFakeServer()
	defer srv.Close()
	t.Setenv("PRINCIPAL_USER", user)
	t.Setenv("PRINCIPAL_PASSWORD", password)
	cfg := testConfig(srv.URL)
	cfg.Auth.Body = map[string]any{"username": "${env.PRINCIPAL_USER}", "password": "${env.PRINCIPAL_PASSWORD}"}
	c := normalized(t, &chain.Chain{Name: "principal", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w", "kind": "KIND_A"}, Expect: okExpect()},
	}})
	rec, err := profileRunner(t, srv, cfg).Run(context.Background(), c, runner.Options{Redact: cfg.Redact})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Steps[0].AuthPrincipal, string(raw)
}

func TestEachStepRecordsWhichPrincipalItsProfileLoggedInAs(t *testing.T) {
	admin, raw := principalOf(t, "admin-user", "pw-one-111")
	if admin == "" {
		t.Fatalf("a step under an auth profile must record which principal the profile logged in as: %s", raw)
	}
	if strings.Contains(raw, "pw-one-111") {
		t.Fatalf("the principal identity must keep the secret out of the record: %s", raw)
	}
	again, _ := principalOf(t, "admin-user", "pw-two-222")
	if again != admin {
		t.Fatalf("a rotated password is the same principal, got %q and %q", admin, again)
	}
	clerk, _ := principalOf(t, "clerk-user", "pw-one-111")
	if clerk == admin {
		t.Fatalf("another username behind the same profile is another principal, both recorded %q", admin)
	}
}
