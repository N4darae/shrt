package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLISliceVerifyThatReachesAVerdictDoesNotCallItAHypothesis(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-thing-flow", "-step", "fetch", "-verify", "-run", "latest"})
	})
	if err != nil || !strings.Contains(out, "verify reproduced") {
		t.Fatalf("the slice keeps every write, so it must reproduce: %v\n%s", err, out)
	}
	if strings.Contains(out, "HYPOTHESIS") {
		t.Fatalf("a verdict was reached, so the hypothesis line contradicts it:\n%s", out)
	}
}

func TestCLISliceDoesNotCountTheLoginAsADroppedWrite(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	appendAuth(t, "${env.WIDGET_USER}")
	writeFile(t, ".shrt/chains/cli-login-flow.yaml", `apiVersion: shrt/v1
name: cli-login-flow
steps:
    - id: login
      call: shrt.test.v1.AuthService/Login
      skip_auth: true
      body:
          username: someone
          password: secret
    - id: partner_login
      call: PartnerAuthService/Login
      skip_auth: true
      body:
          username: someone
          password: secret
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
`)
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-login-flow", "-step", "fetch"})
	})
	if err != nil {
		t.Fatal(err)
	}
	dropped := out[strings.Index(out, "Dropped write steps"):]
	if strings.Contains(dropped, " login ") || strings.Contains(out, "1  login") {
		t.Fatalf("the configured login builds no backend state and must not be a dropped write:\n%s", out)
	}
	if !strings.Contains(dropped, "partner_login") {
		t.Fatalf("a login rpc the config does not name is still a write:\n%s", out)
	}
}
