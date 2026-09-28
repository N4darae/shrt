package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestCouldNotVerifySaysAFreshTokenRefusedMayBeAnAuthRegression(t *testing.T) {
	rec := &runner.Record{Steps: []*runner.StepRecord{
		{ID: "seed", Status: runner.StatusPassed, HTTPStatus: 200},
		{ID: "add", Status: runner.StatusError, HTTPStatus: 401, AuthRetry: runner.AuthRetryNotResent,
			Error: "unauthenticated: token rejected\n       the backend refused a token that a login in this run had just issued: " +
				"the credentials work and the token is current, so this may be an auth regression in the backend"},
	}}
	err := couldNotVerifyAfter("order", "add", "the backend refused authentication: unauthenticated: token rejected", rec, nil)
	if exitCodeOf(err) != 3 {
		t.Fatalf("still exit 3, got %d", exitCodeOf(err))
	}
	msg := err.Error()
	if !strings.Contains(msg, "may be an auth regression") || !strings.Contains(msg, "login in this run") ||
		strings.Contains(msg, "not a verdict about the backend") || strings.Contains(msg, "fix the credentials") {
		t.Fatalf("a fresh token refused is evidence against the backend, and the message must say so plainly: %s", msg)
	}
}
