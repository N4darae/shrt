package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestCLIRunNamesEveryUnsetEnvVarTheAuthBodyReads(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	appendAuth(t, "${env.WIDGET_USER}")
	for _, name := range []string{"WIDGET_USER", "WIDGET_PASSWORD"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}

	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow"}) })
	if err == nil {
		t.Fatalf("a login with no credentials cannot run:\n%s", out)
	}
	both := out + err.Error()
	for _, want := range []string{"WIDGET_USER", "WIDGET_PASSWORD"} {
		if !strings.Contains(both, want) {
			t.Fatalf("both env vars the auth body reads are unset, so both must be named; %s is missing:\n%s", want, both)
		}
	}
}

func TestCLIRunUnsetEnvRefusalNamesTheConfiguredProfile(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	appendAuth(t, "${env.WIDGET_USER}")
	t.Setenv("WIDGET_USER", "")
	if err := os.Unsetenv("WIDGET_USER"); err != nil {
		t.Fatal(err)
	}
	var err error
	captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow"}) })
	if err == nil {
		t.Fatal("a login with an unset env var cannot run")
	}
	if !strings.Contains(err.Error(), `auth profile "default"`) || strings.Contains(err.Error(), "shared (default)") {
		t.Fatalf("the refusal must name the profile as configured, default:\n%v", err)
	}
}
