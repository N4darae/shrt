package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

func chainWithVars(t *testing.T, redact []string, vars map[string]any, secretVar string) *chain.Chain {
	t.Helper()
	c := &chain.Chain{
		Name:   "login-with-vars",
		Redact: redact,
		Vars:   vars,
		Steps: []*chain.Step{{
			ID: "login", Call: "AuthService/Login", SkipAuth: true,
			Body: map[string]any{
				"username": "${vars.staff_user}",
				"password": "${vars." + secretVar + "}",
			},
		}},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAVarNamedPasswordIsRedactedFromTheRecordButStillResolves(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := chainWithVars(t, []string{"**.password"},
		map[string]any{"password": "hunter2", "staff_user": "alice"}, "password")
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}

	srv.mu.Lock()
	sent := srv.bodies[0]
	srv.mu.Unlock()
	if sent["password"] != "hunter2" {
		t.Fatalf("redacting a var must not change what is sent, server saw %v", sent["password"])
	}

	if got := rec.Vars["password"]; got != pathmask.MaskRedacted {
		t.Errorf("rec.Vars[password] = %v, want %s — the redactor ran on the request and the response but never on the vars, so a secret var was written to disk verbatim, and .shrt/runs/ is gitignored so nobody reviews it", got, pathmask.MaskRedacted)
	}
	if got := rec.Vars["staff_user"]; got != "alice" {
		t.Errorf("rec.Vars[staff_user] = %v, want alice — only vars matching a redact pattern may be masked", got)
	}
}

func TestTheDefaultRedactListCoversAPrefixedPasswordVar(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := chainWithVars(t, config.DefaultRedact(),
		map[string]any{"staff_password": "hunter2", "staff_user": "alice"}, "staff_password")
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}
	if got := rec.Vars["staff_password"]; got != pathmask.MaskRedacted {
		t.Errorf("rec.Vars[staff_password] = %v, want %s — '**.password' matches a segment named exactly password, so every differently-prefixed name leaked", got, pathmask.MaskRedacted)
	}
}

func TestVarsRedactionCoversVarsPassedAsOptions(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := chainWithVars(t, config.DefaultRedact(), map[string]any{"staff_user": "alice"}, "staff_password")
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{
		Vars: map[string]any{"staff_password": "from-the-flag"},
	})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}
	if strings.Contains(mustMarshal(t, rec.Vars), "from-the-flag") {
		t.Errorf("a secret passed with -var must be redacted too, got %s", mustMarshal(t, rec.Vars))
	}
}

func TestChangePasswordFieldsAreRedactedFromTheRequest(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := &chain.Chain{
		Name:   "change-password",
		Redact: config.DefaultRedact(),
		Steps: []*chain.Step{{
			ID: "change", Call: "AuthService/Login", SkipAuth: true,
			Body: map[string]any{
				"username":     "alice",
				"old_password": "hunter2",
				"new_password": "hunter3",
			},
		}},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}
	change, _ := rec.Step("change")
	for _, secret := range []string{"hunter2", "hunter3"} {
		if strings.Contains(string(change.Request), secret) {
			t.Errorf("%q reached the run record: %s — this is the iam-changestaffpassword leak, and it survived the redactor because '**.password' does not match new_password or old_password", secret, change.Request)
		}
	}

	srv.mu.Lock()
	sent := srv.bodies[0]
	srv.mu.Unlock()
	if sent["old_password"] != "hunter2" || sent["new_password"] != "hunter3" {
		t.Fatalf("redaction must not change what is sent, server saw %v / %v", sent["old_password"], sent["new_password"])
	}
}
