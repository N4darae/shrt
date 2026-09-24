package runner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

const scrubPassword = "hunter2-pw-9f3a"

func scrubRunner(t *testing.T, srv *fakeServer) *runner.Runner {
	t.Helper()
	t.Setenv("SHRT_TEST_SCRUB_PW", scrubPassword)
	cfg := testConfig(srv.URL)
	cfg.Auth.Body = map[string]any{"username": "staff", "password": "${env.SHRT_TEST_SCRUB_PW}"}
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, ValidateOutput: true, Auth: deps.Bindings}
}

func recordText(t *testing.T, rec *runner.Record) string {
	t.Helper()
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAPasswordAndAReusedTokenNeverReachTheRecord(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	r := scrubRunner(t, srv)

	c := normalized(t, &chain.Chain{Name: "secrets", Redact: []string{"**.access_token", "**.*password"}, Steps: []*chain.Step{
		{ID: "login", Call: "AuthService/Login",
			Body:   map[string]any{"username": "staff", "password": "${env.SHRT_TEST_SCRUB_PW}"},
			Export: map[string]string{"tok": "access_token"}, Expect: okExpect()},
		{ID: "echo_token", Call: "ThingService/Fetch", Body: map[string]any{"id": "${tok}"},
			Expect: []chain.Expectation{{Path: "id", Equals: "${tok}"}, {Path: "id", Contains: "${login.access_token}"}}},
		{ID: "echo_password", Call: "ThingService/Fetch", Body: map[string]any{"id": "${env.SHRT_TEST_SCRUB_PW} x"},
			Expect: []chain.Expectation{{Path: "id", Contains: "${env.SHRT_TEST_SCRUB_PW}"}}},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{Redact: c.Redact})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusPassed {
		t.Fatalf("the chain should pass, scrubbing is applied after evaluation: %s %s", rec.Status, rec.Failure)
	}
	token := srv.tokenIssuedTo(t, "staff")
	text := recordText(t, rec)
	for _, secret := range []string{scrubPassword, token} {
		if strings.Contains(text, secret) {
			t.Fatalf("%q is a credential shrt resolved or received, and it reached the run record: %s", secret, text)
		}
	}
	if got := string(rec.Steps[2].Request); !strings.Contains(got, "<redacted> x") {
		t.Fatalf("the password embedded in a longer value should be replaced in place, got %s", got)
	}
}

func TestATokenTheMiddlewareFetchedIsScrubbedToo(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	r := scrubRunner(t, srv)

	c := normalized(t, &chain.Chain{Name: "mw-token", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "token-1"}, Expect: okExpect()},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	token := srv.tokenIssuedTo(t, "staff")
	if text := recordText(t, rec); strings.Contains(text, token) {
		t.Fatalf("the bearer token %q the auth middleware obtained reached the run record: %s", token, text)
	}
}
