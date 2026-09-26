package runner_test

import (
	"context"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func principalUnder(t *testing.T, user string, extraRedact ...string) string {
	t.Helper()
	srv := newFakeServer()
	defer srv.Close()
	t.Setenv("PRINCIPAL_USER", user)
	t.Setenv("PRINCIPAL_PASSWORD", "pw-one-111")
	cfg := testConfig(srv.URL)
	cfg.Auth.Body = map[string]any{"username": "${env.PRINCIPAL_USER}", "password": "${env.PRINCIPAL_PASSWORD}"}
	cfg.Redact = append(append([]string{}, cfg.Redact...), extraRedact...)
	c := normalized(t, &chain.Chain{Name: "principal", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w", "kind": "KIND_A"}, Expect: okExpect()},
	}})
	rec, err := profileRunner(t, srv, cfg).Run(context.Background(), c, runner.Options{Redact: cfg.Redact})
	if err != nil {
		t.Fatal(err)
	}
	return rec.Steps[0].AuthPrincipal
}

func TestThePrincipalDigestDoesNotDependOnTheRedactList(t *testing.T) {
	admin := principalUnder(t, "admin-user")
	adminRedacted := principalUnder(t, "admin-user", "**.username")
	clerkRedacted := principalUnder(t, "clerk-user", "**.username")
	if adminRedacted == "" || clerkRedacted == "" {
		t.Fatalf("a principal must still be recorded when username is redacted: %q %q", adminRedacted, clerkRedacted)
	}
	if adminRedacted == clerkRedacted {
		t.Fatalf("redacting username must not make two accounts one principal, both %q", adminRedacted)
	}
	if admin != adminRedacted {
		t.Fatalf("the same account is the same principal whatever redact says, got %q and %q", admin, adminRedacted)
	}
}
