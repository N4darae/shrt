package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func appendAuth(t *testing.T, username string) {
	t.Helper()
	path := filepath.Join(".shrt", "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	auth := `auth:
    call: AuthService/Login
    body:
        username: ` + username + `
        password: ${env.WIDGET_PASSWORD}
    token_path: access_token
`
	if err := os.WriteFile(path, append(raw, []byte(auth)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChainLintFailsOnAnAuthBodyReferenceTheLoginCannotResolve(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	appendAuth(t, "${vars.widget_user}")

	err := chainLint(nil)
	if err == nil || !strings.Contains(err.Error(), "lint error") {
		t.Fatalf("an auth body is resolved with no vars, so ${vars.widget_user} kills every run at its first "+
			"step; lint must fail on it before then, got %v", err)
	}
}

func TestChainLintAcceptsAnAuthBodyThatReadsOnlyTheEnvironment(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	appendAuth(t, "${env.WIDGET_USER}")

	if err := chainLint(nil); err != nil {
		t.Fatalf("${env.*} is what an auth body is for: %v", err)
	}
}
