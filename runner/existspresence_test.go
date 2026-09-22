package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func loginChain(expect ...chain.Expectation) *chain.Chain {
	c := &chain.Chain{
		Name: "login-probe",
		Steps: []*chain.Step{{
			ID:        "login",
			Call:      "AuthService/Login",
			SkipAuth:  true,
			AllowFail: true,
			Body:      map[string]any{"username": "admin", "password": "wrong"},
			Expect:    expect,
		}},
	}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func expectResult(t *testing.T, rec *runner.Record, path string) chain.ExpectResult {
	t.Helper()
	for _, sr := range rec.Steps {
		for _, e := range sr.Expect {
			if e.Path == path {
				return e
			}
		}
	}
	t.Fatalf("no expectation recorded for %q", path)
	return chain.ExpectResult{}
}

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

func TestExistsFailsOnAScalarTheServerDidNotSend(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.refuseLogin = true

	c := loginChain(
		chain.Expectation{Path: "error.code", Equals: "unauthenticated"},
		chain.Expectation{Path: "access_token", Exists: yes()},
	)
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := expectResult(t, rec, "access_token")
	if got.Passed {
		t.Fatalf("a refused login sent {\"error\": ...} and nothing else, yet asserting that it carried " +
			"a token PASSED. Every scalar of a described message is materialised at its zero value " +
			"before expectations run, so exists: true is unconditionally true and a chain built on it " +
			"proves nothing")
	}
	if rec.Passed() {
		t.Fatal("the chain must fail, not just the one expectation")
	}
}

func TestExistsPassesOnAScalarTheServerDidSend(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := loginChain(
		chain.Expectation{Path: "error.code", Equals: "OK"},
		chain.Expectation{Path: "access_token", Exists: yes()},
	)
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("a login that did issue a token must satisfy exists: true, otherwise the fix for the "+
			"false green has made exists unusable: %s", rec.Failure)
	}
}

func TestExistsFalsePassesOnAScalarTheServerWithheld(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.refuseLogin = true

	c := loginChain(
		chain.Expectation{Path: "error.code", Equals: "unauthenticated"},
		chain.Expectation{Path: "access_token", Exists: no()},
	)
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("exists: false is how a chain states that a refusal handed back no credential, and it "+
			"was unsatisfiable for every scalar of a described message: %s", rec.Failure)
	}
}

func TestRecordStillCarriesTheZeroValuedFields(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.refuseLogin = true

	c := loginChain(chain.Expectation{Path: "error.code", Equals: "unauthenticated"})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	body := string(rec.Steps[0].Response)
	for _, want := range []string{`"access_token"`, `"expires_at"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("the stored response must keep every declared field so the differ compares a stable "+
				"shape across runs; %s is missing from %s", want, body)
		}
	}
}
