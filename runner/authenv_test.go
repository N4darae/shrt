package runner_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestARunWhoseLoginReadsAnUnsetEnvVarSendsNothing(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	t.Setenv("SHRT_TEST_AUTHENV_UNSET_7F", "x")
	os.Unsetenv("SHRT_TEST_AUTHENV_UNSET_7F")

	cfg := testConfig(srv.URL)
	cfg.Auth.Body = map[string]any{"username": "staff", "password": "${env.SHRT_TEST_AUTHENV_UNSET_7F}"}
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, Auth: deps.Bindings, AuthRoute: deps.Route}

	c := normalized(t, &chain.Chain{Name: "login-env", Steps: []*chain.Step{
		{ID: "probe", Call: "ThingService/Create", SkipAuth: true,
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "create", Call: "ThingService/Create",
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
	}})
	_, err = r.Run(context.Background(), c, runner.Options{})
	if err == nil || !strings.Contains(err.Error(), "SHRT_TEST_AUTHENV_UNSET_7F") || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("step create needs a login whose body reads an unset env var; the run must be refused "+
			"before step probe is sent, got %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.calls) != 0 {
		t.Fatalf("nothing may be sent before the refusal, the backend received %v", srv.calls)
	}
}

func TestARunWhoseStepsNeedNoLoginIgnoresTheLoginEnv(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	t.Setenv("SHRT_TEST_AUTHENV_UNSET_8A", "x")
	os.Unsetenv("SHRT_TEST_AUTHENV_UNSET_8A")

	cfg := testConfig(srv.URL)
	cfg.Auth.Body = map[string]any{"username": "staff", "password": "${env.SHRT_TEST_AUTHENV_UNSET_8A}"}
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, Auth: deps.Bindings, AuthRoute: deps.Route}
	c := normalized(t, &chain.Chain{Name: "no-login", Steps: []*chain.Step{
		{ID: "probe", Call: "ThingService/Create", SkipAuth: true,
			Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "unauthenticated"}}},
	}})
	if _, err := r.Run(context.Background(), c, runner.Options{}); err != nil {
		t.Fatalf("no step of this chain logs in, so the login's env is not needed: %v", err)
	}
}
