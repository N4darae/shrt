package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setFetchAuth(t *testing.T, profile string) {
	t.Helper()
	path := filepath.Join(".shrt", "chains", "cli-thing-flow.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	withProfile := strings.Replace(string(raw), "      call: ThingService/Fetch\n",
		"      call: ThingService/Fetch\n      auth: "+profile+"\n", 1)
	if err := os.WriteFile(path, []byte(withProfile), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChainLintFailsOnAnAuthProfileTheConfigDoesNotDefine(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	appendAuth(t, "${env.WIDGET_USER}")
	setFetchAuth(t, "nosuch")

	var err error
	out := captureStdout(t, func() { err = chainLint([]string{"-strict"}) })
	if err == nil || !strings.Contains(err.Error(), "lint error") {
		t.Fatalf("run refuses auth: nosuch before sending anything, so lint must fail on it too, got %v\n%s", err, out)
	}
	if !strings.Contains(out, `"nosuch"`) || !strings.Contains(out, "have: default") {
		t.Fatalf("the error must name the undefined profile and the ones the config has:\n%s", out)
	}
}

func TestChainLintAcceptsTheReservedInvalidProfile(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	appendAuth(t, "${env.WIDGET_USER}")
	setFetchAuth(t, "invalid")

	out := captureStdout(t, func() { _ = chainLint(nil) })
	if strings.Contains(out, "does not define") {
		t.Fatalf("auth: invalid is reserved and needs no profile:\n%s", out)
	}
}
