package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func TestATokenAnInChainLoginReturnedIsScrubbedWhenReadDirectly(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	r := scrubRunner(t, srv)
	c := normalized(t, &chain.Chain{Name: "direct-token", Steps: []*chain.Step{
		{ID: "login_other", Call: "AuthService/Login", SkipAuth: true,
			Body: map[string]any{"username": "other", "password": "pw-other"}, Expect: okExpect()},
		{ID: "echo_token", Call: "ThingService/Fetch", Body: map[string]any{"id": "${login_other.access_token}"},
			Expect: []chain.Expectation{{Path: "id", Equals: "${login_other.access_token}"}}},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{Redact: config.DefaultRedact()})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusPassed {
		t.Fatalf("the chain should pass: %s %s", rec.Status, rec.Failure)
	}
	token := srv.tokenIssuedTo(t, "other")
	if text := recordText(t, rec); strings.Contains(text, token) {
		t.Fatalf("the token %q an in-chain login returned reached the run record: %s", token, text)
	}
	if got := string(rec.Steps[1].Request); !strings.Contains(got, "<redacted>") {
		t.Fatalf("the token read by direct reference should be replaced, got %s", got)
	}
}

func TestATokenAtAnUnredactedTokenPathIsScrubbed(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	r := scrubRunner(t, srv)
	c := normalized(t, &chain.Chain{Name: "direct-token", Steps: []*chain.Step{
		{ID: "login_other", Call: "AuthService/Login", SkipAuth: true,
			Body: map[string]any{"username": "other", "password": "pw-other"}, Expect: okExpect()},
		{ID: "echo_token", Call: "ThingService/Fetch", Body: map[string]any{"id": "${login_other.access_token}"}, Expect: okExpect()},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{Redact: []string{"**.*password"}})
	if err != nil {
		t.Fatal(err)
	}
	token := srv.tokenIssuedTo(t, "other")
	if text := recordText(t, rec); strings.Contains(text, token) {
		t.Fatalf("a token at the login rpc's token_path is a secret even when no redact pattern covers it: %s", text)
	}
}
