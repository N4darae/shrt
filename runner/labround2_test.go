package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func tagChain(t *testing.T) *chain.Chain {
	return normalized(t, &chain.Chain{Name: "tagged", Steps: []*chain.Step{
		{ID: "login", Call: "AuthService/Login", SkipAuth: true,
			Body: map[string]any{"username": "staff", "password": "secret"}, Expect: okExpect()},
		{ID: "create", Call: "ThingService/Create",
			Body: map[string]any{"name": "w-${vars.tag}", "kind": "KIND_A"}, Expect: okExpect()},
	}})
}

func TestARunReadingAnUnsuppliedVarSendsNothing(t *testing.T) {
	for _, dry := range []bool{false, true} {
		srv := newFakeServer()
		_, err := newRunner(t, srv).Run(context.Background(), tagChain(t), runner.Options{DryRun: dry})
		srv.mu.Lock()
		sent := len(srv.calls)
		srv.mu.Unlock()
		srv.Close()
		if err == nil {
			t.Fatalf("dry=%v: a chain reading ${vars.tag} ran without -var tag", dry)
		}
		if !strings.Contains(err.Error(), "${vars.tag}") || !strings.Contains(err.Error(), "-var tag=...") {
			t.Errorf("dry=%v: error must name the var and the flag that supplies it: %v", dry, err)
		}
		if sent != 0 {
			t.Errorf("dry=%v: %d request(s) reached the server before the run refused; the login must not be sent", dry, sent)
		}
	}
}

func TestARunWithTheVarSuppliedOrDeclaredIsNotRefused(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	rec, err := newRunner(t, srv).Run(context.Background(), tagChain(t), runner.Options{Vars: map[string]any{"tag": "x"}})
	if err != nil || !rec.Passed() {
		t.Fatalf("supplied var: err=%v rec=%+v", err, rec)
	}
	c := tagChain(t)
	c.Vars = map[string]any{"tag": "default"}
	if _, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{}); err != nil {
		t.Fatalf("declared var: %v", err)
	}
}

func TestAStepWithNoExpectRefusedInBandCarriesAWarning(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.createRefusal = "PERMISSION_DENIED"
	c := normalized(t, &chain.Chain{Name: "unasserted", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w", "kind": "KIND_A"}},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("a step with no expect is still recorded passed, got %s", rec.Status)
	}
	w := stepByID(t, rec, "create").Warning
	if !strings.Contains(w, "refused in-band") || !strings.Contains(w, "PERMISSION_DENIED") {
		t.Fatalf("warning = %q, want it to say the step was refused in-band with the code", w)
	}

	srv.createRefusal = ""
	rec, err = newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if w := stepByID(t, rec, "create").Warning; w != "" {
		t.Fatalf("an OK envelope must not warn, got %q", w)
	}
}

func TestAStepThatAssertsTheInBandRefusalDoesNotWarn(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.createRefusal = "PERMISSION_DENIED"
	c := normalized(t, &chain.Chain{Name: "asserted", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "PERMISSION_DENIED"}}},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("err=%v status=%v", err, rec)
	}
	if w := stepByID(t, rec, "create").Warning; w != "" {
		t.Fatalf("an asserted refusal must not warn, got %q", w)
	}
}

func TestALoginSeedingOneProfileDoesNotAlsoClaimItFailedToSeedAnother(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	c := normalized(t, &chain.Chain{Name: "checker-login", Steps: []*chain.Step{
		{ID: "checker_login", Call: "AuthService/Login", SkipAuth: true,
			Body: map[string]any{"username": "checker", "password": "secret"}, Expect: okExpect()},
	}})
	rec, err := profileRunner(t, srv, sharedProcedureConfig(srv.URL)).Run(context.Background(), c, runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("err=%v rec=%v", err, rec)
	}
	note := stepByID(t, rec, "checker_login").Note
	if !strings.Contains(note, "seeded the checker auth token") {
		t.Fatalf("note = %q, want it to say the checker token was seeded", note)
	}
	if strings.Contains(note, "did not seed") || strings.Contains(note, "declare a profile") {
		t.Fatalf("note = %q: a login that seeded a profile must not also read as a failed seed", note)
	}
	if !strings.Contains(note, "shared (default)") {
		t.Fatalf("note = %q, want it to say the shared (default) profile was left unchanged", note)
	}
	if strings.HasSuffix(strings.TrimSpace(note), "auth:") {
		t.Fatalf("note ends mid-sentence: %q", note)
	}
}

func TestALoginMatchingNoProfileNamesEveryProfileOnceAndEndsWithTheFix(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	c := normalized(t, &chain.Chain{Name: "stranger-login", Steps: []*chain.Step{
		{ID: "stranger_login", Call: "AuthService/Login", SkipAuth: true,
			Body: map[string]any{"username": "subject", "password": "secret"}, Expect: okExpect()},
	}})
	rec, err := profileRunner(t, srv, sharedProcedureConfig(srv.URL)).Run(context.Background(), c, runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("err=%v rec=%v", err, rec)
	}
	note := stepByID(t, rec, "stranger_login").Note
	if strings.Count(note, "did not seed") != 1 {
		t.Fatalf("note = %q, want exactly one did-not-seed sentence", note)
	}
	if !strings.Contains(note, "every profile it could seed (checker, shared (default))") {
		t.Fatalf("note = %q, want it to list both profiles as the ones it could have seeded", note)
	}
	if !strings.Contains(note, "auth: <that profile's name>") {
		t.Fatalf("note = %q, want it to end with how to name the new profile", note)
	}
}
