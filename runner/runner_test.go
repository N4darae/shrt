package runner_test

import (
	"context"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func testConfig(baseURL string) *config.Config {
	cfg := config.Default()
	cfg.Target.BaseURL = baseURL
	cfg.Auth = &config.Auth{
		Call:        "AuthService/Login",
		Body:        map[string]any{"username": "staff", "password": "secret"},
		TokenPath:   "access_token",
		ExpiresPath: "expires_at",
	}
	return cfg
}

func testChain() *chain.Chain {
	c := &chain.Chain{
		Name:     "thing-flow",
		Volatile: []string{"**.created_at", "**.id"},
		Steps: []*chain.Step{
			{
				ID:   "create",
				Call: "ThingService/Create",
				Body: map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"},
				Expect: []chain.Expectation{
					{Path: "error.code", Equals: "OK"},
					{Path: "id", NotEmpty: true},
				},
				Export: map[string]string{"thing_id": "id"},
			},
			{
				ID:   "fetch",
				Call: "ThingService/Fetch",
				Body: map[string]any{"id": "${create.id}"},
				Expect: []chain.Expectation{
					{Path: "error.code", Equals: "OK"},
					{Path: "name", Equals: "widget"},
				},
			},
		},
	}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func newRunner(t *testing.T, srv *fakeServer) *runner.Runner {
	t.Helper()
	cat := catalogtest.New()
	deps, err := runner.Build(context.Background(), testConfig(srv.URL), cat, nil)
	if err != nil {
		t.Fatalf("build deps: %v", err)
	}
	return &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, ValidateOutput: true, Auth: deps.Bindings}
}

func TestRunPassesAndExports(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("want passed, got %s: %s", rec.Status, rec.Failure)
	}
	if got := rec.Exports["thing_id"]; got != "thing-1" {
		t.Fatalf("export thing_id = %v, want thing-1", got)
	}
	if len(rec.Steps) != 2 {
		t.Fatalf("want 2 step records, got %d", len(rec.Steps))
	}
	if srv.loginCount() != 1 {
		t.Fatalf("want exactly 1 login, got %d", srv.loginCount())
	}
}

func TestRunStopsAtFirstFailedStep(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := testChain()
	c.Steps[0].Expect = []chain.Expectation{{Path: "id", Equals: "wrong-id"}}

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("want failure, got passed")
	}
	if len(rec.Steps) != 1 {
		t.Fatalf("want the chain to stop after step 1, got %d step records", len(rec.Steps))
	}
}

func TestExpiredTokenRefreshesInsteadOfFailing(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	r := newRunner(t, srv)
	srv.expireTokens()

	rec, err := r.Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("an expired token must not fail the chain, got %s: %s", rec.Status, rec.Failure)
	}
	if srv.loginCount() != 2 {
		t.Fatalf("want a second login after the rejection, got %d", srv.loginCount())
	}
}

func TestUnknownRequestFieldFailsBeforeSending(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := testChain()
	c.Steps[0].Body["not_a_field"] = "x"

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Steps[0].Status != runner.StatusError {
		t.Fatalf("want a validation error, got %s", rec.Steps[0].Status)
	}
	for _, path := range srv.calls {
		if path == "/shrt.test.v1.ThingService/Create" {
			t.Fatal("an invalid request must not reach the server")
		}
	}
}

func TestDryRunSendsNoTrafficAndStillResolvesReferences(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), testChain(), runner.Options{DryRun: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("dry run should pass, got %s: %s", rec.Status, rec.Failure)
	}
	if len(rec.Steps) != 2 {
		t.Fatalf("dry run must visit every step, got %d", len(rec.Steps))
	}
	for _, path := range srv.calls {
		if path != "/shrt.test.v1.AuthService/Login" {
			t.Fatalf("dry run sent traffic to %s", path)
		}
	}
}
