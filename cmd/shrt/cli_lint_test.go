package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func appendAuth(t *testing.T, username string) {
	t.Helper()
	path := filepath.Join(".shrt", "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	auth := "auth:\n    call: AuthService/Login\n    body:\n        username: " + username + "\n        password: ${env.WIDGET_PASSWORD}\n    token_path: access_token\n"
	if err := os.WriteFile(path, append(raw, []byte(auth)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cliEdit(t *testing.T, path, from, to string) {
	t.Helper()
	raw := string(mustRead(t, path))
	if !strings.Contains(raw, from) {
		t.Fatalf("%s has no %q", path, from)
	}
	writeFile(t, path, strings.Replace(raw, from, to, 1))
}

func cliUnsetEnv(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

const cliFlow = ".shrt/chains/cli-thing-flow.yaml"

func TestChainLintJudgesTheAuthAndTheWarningsOfEachChain(t *testing.T) {
	for _, c := range []struct {
		name    string
		setup   func(t *testing.T)
		args    []string
		failing bool
		want    []string
		not     []string
	}{
		{name: "an auth body reading a var the login cannot resolve", failing: true,
			setup: func(t *testing.T) { appendAuth(t, "${vars.widget_user}") }},
		{name: "an auth body reading only the environment",
			setup: func(t *testing.T) {
				appendAuth(t, "${env.WIDGET_USER}")
				t.Setenv("WIDGET_USER", "u")
				t.Setenv("WIDGET_PASSWORD", "p")
			}},
		{name: "an auth profile the config does not define", args: []string{"-strict"}, failing: true, want: []string{`"nosuch"`, "have: default"},
			setup: func(t *testing.T) {
				appendAuth(t, "${env.WIDGET_USER}")
				cliEdit(t, cliFlow, "      call: ThingService/Fetch\n", "      call: ThingService/Fetch\n      auth: nosuch\n")
			}},
		{name: "the reserved invalid profile", not: []string{"does not define"},
			setup: func(t *testing.T) {
				appendAuth(t, "${env.WIDGET_USER}")
				cliEdit(t, cliFlow, "      call: ThingService/Fetch\n", "      call: ThingService/Fetch\n      auth: invalid\n")
			}},
		{name: "an Authorization written into target.headers", failing: true,
			setup: func(t *testing.T) {
				appendAuth(t, "${env.WIDGET_USER}")
				cliEdit(t, ".shrt/config.yaml", "target:\n", "target:\n    headers:\n        authorization: Bearer hand-written\n")
			}},
		{name: "another target header",
			setup: func(t *testing.T) {
				appendAuth(t, "${env.WIDGET_USER}")
				cliEdit(t, ".shrt/config.yaml", "target:\n", "target:\n    headers:\n        X-Tenant: acme\n")
			}},
		{name: "login variables unset for two chains", want: []string{"WIDGET_PASSWORD, WIDGET_USER", "the 2 chain(s)", "warn ", "not exported in this shell"},
			setup: func(t *testing.T) {
				appendAuth(t, "${env.WIDGET_USER}")
				cliUnsetEnv(t, "WIDGET_USER", "WIDGET_PASSWORD")
				writeFile(t, ".shrt/chains/cli-thing-again.yaml", strings.Replace(string(mustRead(t, cliFlow)), "name: cli-thing-flow", "name: cli-thing-again", 1))
			}},
		{name: "unasserted timestamps in two chains", want: []string{"WARN   timestamps unasserted in 2 chain(s), created_at (", "verify masks timestamps", "\n2 chain(s): 2 with warnings\n"},
			setup: func(t *testing.T) {
				writeFile(t, ".shrt/chains/cli-thing-copy.yaml", strings.Replace(string(mustRead(t, cliFlow)), "name: cli-thing-flow", "name: cli-thing-copy", 1))
			}},
		{name: "unasserted timestamps with -v", args: []string{"-v"}, want: []string{"[fetch] timestamp created_at unasserted; expect within:", "\nWARN   cli-thing-flow ["}},
		{name: "an asserted timestamp", args: []string{"-v", "cli-thing-flow"}, want: []string{"ok   cli-thing-flow\n", "1 chain(s) lint clean\n"}, not: []string{"timestamp created_at unasserted"},
			setup: func(t *testing.T) {
				cliEdit(t, cliFlow, "            equals: widget\n", "            equals: widget\n          - path: created_at\n            lte: ${nowunix}\n")
			}},
		{name: "a clean chain prints the count only", not: []string{"ok   ", "WARN"},
			setup: func(t *testing.T) {
				cliEdit(t, cliFlow, "            equals: widget\n", "            equals: widget\n          - path: created_at\n            lte: ${nowunix}\n")
				writeFile(t, ".shrt/chains/cli-thing-copy.yaml", strings.Replace(string(mustRead(t, cliFlow)), "name: cli-thing-flow", "name: cli-thing-copy", 1))
			}, want: []string{"2 chain(s) lint clean\n"}},
		{name: "a failing chain beside a clean one", failing: true, want: []string{"FAIL broken\n", "2 chain(s): 1 failing, 1 with warnings\n"}, not: []string{"ok   "},
			setup: func(t *testing.T) {
				writeFile(t, ".shrt/chains/broken.yaml", "apiVersion: shrt/v1\nname: broken\nsteps:\n  - id: fetch\n    call: shrt.test.v1.ThingService/NoSuchRpc\n    body: {id: x}\n")
			}},
		{name: "a step that asserts nothing", args: []string{"bare"},
			want: []string{"exit 0, but 1 warning(s) above are errors under 'shrt chain lint -strict', which .shrt/ci-gate.sh runs\n"},
			setup: func(t *testing.T) {
				writeFile(t, ".shrt/chains/bare.yaml", "apiVersion: shrt/v1\nname: bare\nsteps:\n  - id: fetch\n    call: shrt.test.v1.ThingService/Fetch\n    body: {id: x}\n")
			}},
		{name: "one warning on three steps", args: []string{"cli-probe"},
			want: []string{"A step with no expect entry", "[fetch_2] asserts nothing at all, as above\n", "[fetch_3] asserts nothing at all, as above\n"},
			setup: func(t *testing.T) {
				steps := ""
				for _, n := range []string{"1", "2", "3"} {
					steps += "    - id: fetch_" + n + "\n      call: ThingService/Fetch\n      body:\n          id: thing-" + n + "\n"
				}
				writeFile(t, ".shrt/chains/cli-probe.yaml", "apiVersion: shrt/v1\nname: cli-probe\nsteps:\n"+steps)
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := newFakeCLIBackend()
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			if c.setup != nil {
				c.setup(t)
			}
			var err error
			out := captureStdout(t, func() { err = chainLint(c.args) })
			if c.failing != (err != nil) || (err != nil && !strings.Contains(err.Error(), "lint error")) {
				t.Fatalf("failing %v, got %v\n%s", c.failing, err, out)
			}
			for _, want := range c.want {
				if !strings.Contains(out, want) {
					t.Errorf("want %q in:\n%s", want, out)
				}
			}
			for _, not := range c.not {
				if strings.Contains(out, not) {
					t.Errorf("want no %q in:\n%s", not, out)
				}
			}
			for _, once := range []string{"not exported in this shell", "verify masks timestamps", "A step with no expect entry"} {
				if strings.Count(out, once) > 1 {
					t.Errorf("%q is explained once per lint:\n%s", once, out)
				}
			}
		})
	}
}

func TestRunRefusesALoginItCannotSendNamingWhatIsMissing(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	appendAuth(t, "${env.WIDGET_USER}")
	cliUnsetEnv(t, "WIDGET_USER", "WIDGET_PASSWORD")
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow"}) })
	if err == nil || !strings.Contains(out+err.Error(), "WIDGET_USER") || !strings.Contains(out+err.Error(), "WIDGET_PASSWORD") ||
		!strings.Contains(err.Error(), `auth profile "default"`) || strings.Contains(err.Error(), "shared (default)") {
		t.Fatalf("both unset env vars and the configured profile are named: %v\n%s", err, out)
	}
	t.Setenv("WIDGET_USER", "u")
	t.Setenv("WIDGET_PASSWORD", "p")
	cliEdit(t, ".shrt/config.yaml", "target:\n", "target:\n    headers:\n        authorization: Bearer hand-written\n")
	err = runRun(context.Background(), []string{"cli-thing-flow", "-quiet"})
	if err == nil || !strings.Contains(err.Error(), "target.headers") || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("a hand-written Authorization in target.headers is refused before sending anything: %v", err)
	}
}
