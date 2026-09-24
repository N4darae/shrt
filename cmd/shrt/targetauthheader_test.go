package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func addTargetHeader(t *testing.T, name, value string) {
	t.Helper()
	path := filepath.Join(".shrt", "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(raw), "target:\n", "target:\n    headers:\n        "+name+": "+value+"\n", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChainLintAndRunRefuseAnAuthorizationWrittenIntoTargetHeaders(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	appendAuth(t, "${env.WIDGET_USER}")
	addTargetHeader(t, "authorization", "Bearer hand-written")
	t.Setenv("WIDGET_USER", "u")
	t.Setenv("WIDGET_PASSWORD", "p")

	err := chainLint(nil)
	if err == nil || !strings.Contains(err.Error(), "lint error") {
		t.Fatalf("a hand-written Authorization in target.headers must be a lint error, got %v", err)
	}
	err = runRun(context.Background(), []string{"cli-thing-flow", "-quiet"})
	if err == nil || !strings.Contains(err.Error(), "target.headers") || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("shrt run must refuse it before sending anything, got %v", err)
	}
}

func TestATargetHeaderThatIsNotTheAuthHeaderIsAccepted(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	appendAuth(t, "${env.WIDGET_USER}")
	addTargetHeader(t, "X-Tenant", "acme")

	if err := chainLint(nil); err != nil {
		t.Fatalf("a header other than the auth header is what target.headers is for: %v", err)
	}
}
