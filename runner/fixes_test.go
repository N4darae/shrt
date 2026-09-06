package runner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

func TestCamelCaseResponsesAreReadableBySnakeCasePaths(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := testChain()
	c.Steps[1].Expect = append(c.Steps[1].Expect,
		chain.Expectation{Path: "created_at", NotEmpty: true},
		chain.Expectation{Path: "createdAt", NotEmpty: true},
	)
	c.Steps[1].Export = map[string]string{"stamp": "created_at"}

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("the server answers camelCase, snake_case paths must still resolve: %s", rec.Failure)
	}
	if got := rec.Exports["stamp"]; got != "stamp-1" {
		t.Fatalf("export via snake_case path = %v, want stamp-1", got)
	}
}

func TestStoredResponsesAreCanonicalisedToProtoNames(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), testChain(), runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}
	fetch, ok := rec.Step("fetch")
	if !ok {
		t.Fatal("no fetch step recorded")
	}
	body := string(fetch.Response)
	if !strings.Contains(body, `"created_at"`) {
		t.Fatalf("stored response should use proto names, got %s", body)
	}
	if strings.Contains(body, `"createdAt"`) {
		t.Fatalf("stored response still carries the wire casing, got %s", body)
	}
}

func TestAuthTokenIsFoundDespiteCamelCaseLoginResponse(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("config says token_path access_token, server answers accessToken: %s", rec.Failure)
	}
	if got := srv.headerFor("/shrt.test.v1.ThingService/Create", "Authorization"); !strings.HasPrefix(got, "Bearer token-") {
		t.Fatalf("Authorization header = %q, want a Bearer token", got)
	}
}

func TestSecretsAreRedactedFromTheRecordButStillSent(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := &chain.Chain{
		Name:   "login-flow",
		Redact: []string{"**.password", "**.access_token"},
		Steps: []*chain.Step{{
			ID: "login", Call: "AuthService/Login", SkipAuth: true,
			Body:   map[string]any{"username": "staff", "password": "hunter2"},
			Export: map[string]string{"token": "access_token"},
		}},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}
	login, _ := rec.Step("login")

	if strings.Contains(string(login.Request), "hunter2") {
		t.Fatalf("the password must not reach the run record: %s", login.Request)
	}
	if !strings.Contains(string(login.Request), "<redacted>") {
		t.Fatalf("want a redaction marker, got %s", login.Request)
	}
	if strings.Contains(string(login.Response), "token-1") {
		t.Fatalf("the token must not reach the run record: %s", login.Response)
	}

	var sent map[string]any
	srv.mu.Lock()
	sent = srv.bodies[0]
	srv.mu.Unlock()
	if sent["password"] != "hunter2" {
		t.Fatalf("redaction must not change what is sent, server saw %v", sent["password"])
	}
	if rec.Exports["token"] != pathmask.MaskRedacted {
		t.Fatalf("an exported secret must be masked in the record too, got %v", rec.Exports["token"])
	}
}

func TestStepHeadersResolveReferences(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := testChain()
	c.Steps[1].Headers = map[string]string{"X-Thing-Ref": "ref-${create.id}"}

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}
	got := srv.headerFor("/shrt.test.v1.ThingService/Fetch", "X-Thing-Ref")
	if got != "ref-thing-1" {
		t.Fatalf("header = %q, want ref-thing-1 — the reference was not resolved", got)
	}
}

func TestUnresolvableHeaderFailsTheStep(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := testChain()
	c.Steps[0].Headers = map[string]string{"X-Bad": "${nope.id}"}

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("an unresolvable header must fail the step, not go out literally")
	}
	if !strings.Contains(rec.Steps[0].Error, "X-Bad") {
		t.Fatalf("the error should name the header, got %q", rec.Steps[0].Error)
	}
}

func TestRedactedRecordsStillDiffCleanly(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	r := newRunner(t, srv)
	c := testChain()
	c.Redact = []string{"**.name"}

	first, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil || !first.Passed() {
		t.Fatalf("run: %v", err)
	}
	var decoded map[string]any
	fetch, _ := first.Step("fetch")
	if err := json.Unmarshal(fetch.Response, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["name"] != "<redacted>" {
		t.Fatalf("want name redacted, got %v", decoded["name"])
	}
}

func TestExplicitLoginStepDoesNotCauseASecondLogin(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := &chain.Chain{
		Name:     "login-then-work",
		Volatile: []string{"**.id"},
		Steps: []*chain.Step{
			{
				ID: "login", Call: "AuthService/Login", SkipAuth: true,
				Body:   map[string]any{"username": "staff", "password": "secret"},
				Expect: []chain.Expectation{{Path: "access_token", NotEmpty: true}},
			},
			{
				ID: "create", Call: "ThingService/Create",
				Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
			},
		},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}
	if got := srv.loginCount(); got != 1 {
		t.Fatalf("an explicit login step must seed the shared token, want 1 login, got %d", got)
	}
	login, _ := rec.Step("login")
	if login.Note == "" {
		t.Fatal("the seeding should be recorded on the step so it is visible in the run")
	}
	if got := srv.headerFor("/shrt.test.v1.ThingService/Create", "Authorization"); got != "Bearer token-1" {
		t.Fatalf("the later step should carry the token from the login step, got %q", got)
	}
}

func TestSeededTokenStillRefreshesWhenRejected(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := &chain.Chain{
		Name:     "login-then-work",
		Volatile: []string{"**.id"},
		Steps: []*chain.Step{
			{ID: "login", Call: "AuthService/Login", SkipAuth: true,
				Body: map[string]any{"username": "staff", "password": "secret"}},
			{ID: "create", Call: "ThingService/Create",
				Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	r := newRunner(t, srv)
	srv.expireTokens()

	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("a seeded token that the server rejects must still refresh: %v %s", err, rec.Failure)
	}
	if got := srv.loginCount(); got != 2 {
		t.Fatalf("want the explicit login plus one refresh, got %d", got)
	}
}
