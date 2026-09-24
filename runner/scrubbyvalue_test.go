package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func envLoginRunner(t *testing.T, srv *fakeServer, user string) *runner.Runner {
	t.Helper()
	t.Setenv("SHRT_TEST_SCRUB_USER", user)
	t.Setenv("SHRT_TEST_SCRUB_PW", scrubPassword)
	cfg := testConfig(srv.URL)
	cfg.Auth.Body = map[string]any{"username": "${env.SHRT_TEST_SCRUB_USER}", "password": "${env.SHRT_TEST_SCRUB_PW}"}
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, ValidateOutput: true, Auth: deps.Bindings}
}

func TestALoginUsernameIsNotScrubbedFromOtherValues(t *testing.T) {
	for _, user := range []string{"clerk", "admin"} {
		t.Run(user, func(t *testing.T) {
			srv := newFakeServer()
			defer srv.Close()
			r := envLoginRunner(t, srv, user)
			c := normalized(t, &chain.Chain{Name: "names", Steps: []*chain.Step{
				{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": user + " made", "kind": "KIND_A"}, Expect: okExpect()},
				{ID: "echo_password", Call: "ThingService/Fetch", Body: map[string]any{"id": "${env.SHRT_TEST_SCRUB_PW} x"}, Expect: okExpect()},
			}})
			rec, err := r.Run(context.Background(), c, runner.Options{Redact: config.DefaultRedact()})
			if err != nil {
				t.Fatal(err)
			}
			if got := string(rec.Steps[0].Request); !strings.Contains(got, user+" made") {
				t.Fatalf("a username is not a secret, the request must keep %q, got %s", user+" made", got)
			}
			if text := recordText(t, rec); strings.Contains(text, scrubPassword) {
				t.Fatalf("the login password must still be scrubbed by value: %s", text)
			}
			if got := string(rec.Steps[1].Request); !strings.Contains(got, "<redacted> x") {
				t.Fatalf("the password embedded in a longer value should be replaced in place, got %s", got)
			}
		})
	}
}
