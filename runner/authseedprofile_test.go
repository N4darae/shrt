package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func sharedProcedureConfig(baseURL string) *config.Config {
	cfg := testConfig(baseURL)
	cfg.Auth.Profiles = map[string]*config.Auth{
		"checker": {
			Call:        "AuthService/Login",
			Body:        map[string]any{"username": "checker", "password": "secret"},
			TokenPath:   "access_token",
			ExpiresPath: "expires_at",
		},
	}
	return cfg
}

func TestALoginAsSomeoneElseDoesNotSeedTheProfileTheStepRanAs(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "reauth-flow", Steps: []*chain.Step{
		{ID: "login_subject", Call: "AuthService/Login",
			Body: map[string]any{"username": "subject", "password": "secret"}, Expect: okExpect()},
		{ID: "create", Call: "ThingService/Create",
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
	}})

	rec, err := profileRunner(t, srv, sharedProcedureConfig(srv.URL)).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("want passed, got %s: %s", rec.Status, rec.Failure)
	}

	login := srv.headerFor("/shrt.test.v1.AuthService/Login", "Authorization")
	if login != "" {
		t.Fatalf("the login step must not carry a token, got %q", login)
	}
	subjectToken := srv.tokenIssuedTo(t, "subject")
	staffToken := srv.tokenIssuedTo(t, "staff")
	create := srv.headerFor("/shrt.test.v1.ThingService/Create", "Authorization")
	if create != "Bearer "+staffToken {
		t.Fatalf("the step after a login as subject carries %q, want the configured staff token %q (subject's is %q) — "+
			"a login seeds a profile only when it sent that profile's own credentials, or every later step "+
			"silently acts as whoever logged in last", create, staffToken, subjectToken)
	}

	note := stepByID(t, rec, "login_subject").Note
	if !strings.Contains(note, "did not seed") {
		t.Errorf("login_subject note = %q, want it to say the shared token was not seeded: the record is where "+
			"a reader checks WHICH identity later steps run as", note)
	}
}

func TestALoginStepUnderAProfileSeedsThatProfileNotTheDefault(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "checker-flow", Steps: []*chain.Step{
		{ID: "login_checker", Call: "AuthService/Login", Auth: "checker",
			Body: map[string]any{"username": "checker", "password": "secret"}, Expect: okExpect()},
		{ID: "create", Call: "ThingService/Create",
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
	}})

	rec, err := profileRunner(t, srv, sharedProcedureConfig(srv.URL)).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("want passed, got %s: %s", rec.Status, rec.Failure)
	}

	checkerToken := srv.tokenIssuedTo(t, "checker")
	create := srv.headerFor("/shrt.test.v1.ThingService/Create", "Authorization")
	if strings.Contains(create, checkerToken) {
		t.Fatalf("the default-profile step carries the CHECKER's token %q — seeding must follow the "+
			"step's own profile in both directions, or a checker login quietly takes over every "+
			"later step", checkerToken)
	}
}
