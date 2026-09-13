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

func chainAssertingOnASecretField() *chain.Chain {
	c := &chain.Chain{
		Name:   "assert-on-a-secret",
		Redact: config.DefaultRedact(),
		Steps: []*chain.Step{
			{
				ID: "login", Call: "AuthService/Login", SkipAuth: true,
				Body: map[string]any{"username": "alice", "password": "hunter2"},
				Expect: []chain.Expectation{
					{Path: "error.code", Equals: "OK"},
					{Path: "access_token", NotEmpty: true},
				},
			},
		},
	}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func TestAnExpectationOnASecretFieldIsRedactedFromTheRecord(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), chainAssertingOnASecretField(), runner.Options{})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}

	login, _ := rec.Step("login")
	var tokenExpect *chain.ExpectResult
	for i := range login.Expect {
		if login.Expect[i].Path == "access_token" {
			tokenExpect = &login.Expect[i]
		}
	}
	if tokenExpect == nil {
		t.Fatalf("no expectation recorded for access_token")
	}
	if tokenExpect.Got != pathmask.MaskRedacted {
		t.Errorf("expect[access_token].Got = %v, want %s — the SAME field the stored response already masks was written to the record unredacted via the expectation result", tokenExpect.Got, pathmask.MaskRedacted)
	}
	if strings.Contains(mustMarshal(t, login.Expect), "token-1") {
		t.Errorf("the token survives in the expect array: %s", mustMarshal(t, login.Expect))
	}
}

func TestANonSecretExpectationIsLeftAlone(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), testChain(), runner.Options{
		Redact: config.DefaultRedact(),
	})
	if err != nil || !rec.Passed() {
		t.Fatalf("run: %v %s", err, rec.Failure)
	}
	create, _ := rec.Step("create")
	for _, e := range create.Expect {
		if e.Path == "id" && e.Got == pathmask.MaskRedacted {
			t.Errorf("expect[id].Got was masked, but id matches no redact pattern")
		}
	}
}
