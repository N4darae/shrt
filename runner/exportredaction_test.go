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

func chainExportingASecretThenUsingIt(t *testing.T) *chain.Chain {
	t.Helper()
	c := &chain.Chain{
		Name:   "export-a-secret",
		Redact: config.DefaultRedact(),
		Steps: []*chain.Step{
			{
				ID: "login", Call: "AuthService/Login", SkipAuth: true,
				Body:   map[string]any{"username": "alice", "password": "hunter2"},
				Export: map[string]string{"session": "access_token"},
			},
			{
				ID: "create", Call: "ThingService/Create",
				Body: map[string]any{"name": "${exports.session}", "kind": "KIND_A"},
			},
		},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAnExportedSecretIsRedactedFromTheRecordButStillResolves(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), chainExportingASecretThenUsingIt(t), runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}

	srv.mu.Lock()
	sent := srv.bodies[len(srv.bodies)-1]
	srv.mu.Unlock()
	if sent["name"] != "token-1" {
		t.Fatalf("a later step must still resolve the real exported value, server saw %v", sent["name"])
	}

	if got := rec.Exports["session"]; got != pathmask.MaskRedacted {
		t.Errorf("rec.Exports[session] = %v, want %s — an export lands under its ALIAS, so no '**.access_token' pattern reaches it and the token is written to disk verbatim", got, pathmask.MaskRedacted)
	}
	login, _ := rec.Step("login")
	if got := login.Exported["session"]; got != pathmask.MaskRedacted {
		t.Errorf("step.Exported[session] = %v, want %s — the per-step copy leaks the same value", got, pathmask.MaskRedacted)
	}
	if strings.Contains(mustMarshal(t, rec.Exports)+mustMarshal(t, login.Exported), "token-1") {
		t.Errorf("the token survives somewhere in the record: %s / %s", mustMarshal(t, rec.Exports), mustMarshal(t, login.Exported))
	}
}

func TestANonSecretExportIsLeftAlone(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), testChain(), runner.Options{
		Redact: config.DefaultRedact(),
	})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}
	if got := rec.Exports["thing_id"]; got != "thing-1" {
		t.Errorf("rec.Exports[thing_id] = %v, want thing-1 — only exports whose SOURCE PATH matches a redact pattern may be masked", got)
	}
}
