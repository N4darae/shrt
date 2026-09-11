package transport

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLogin_RetriesAResourceExhaustedRefusal(t *testing.T) {
	calls := 0
	src := NewLoginTokenSource(AuthSpec{
		Procedure: "svc/Login",
		Body:      func() ([]byte, error) { return []byte(`{}`), nil },
		TokenPath: "access_token",
	}, func(ctx context.Context, c *Call) (*Result, error) {
		calls++
		if calls < 3 {
			return &Result{Status: 429, Error: &Error{Code: "resource_exhausted", Message: "rate limit exceeded, retry later"}}, nil
		}
		return &Result{Status: 200, Body: []byte(`{"access_token":"tok"}`)}, nil
	})
	src.RetryBackoff = time.Millisecond

	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("a rate-limited login is a WAIT, not a failure: %v", err)
	}
	if tok != "tok" {
		t.Fatalf("token = %q, want tok", tok)
	}
	if calls != 3 {
		t.Fatalf("login attempts = %d, want 3 — two refusals then success", calls)
	}
}

func TestLogin_DoesNotRetryABadCredential(t *testing.T) {
	calls := 0
	src := NewLoginTokenSource(AuthSpec{
		Procedure: "svc/Login",
		Body:      func() ([]byte, error) { return []byte(`{}`), nil },
		TokenPath: "access_token",
	}, func(ctx context.Context, c *Call) (*Result, error) {
		calls++
		return &Result{Status: 401, Error: &Error{Code: "unauthenticated", Message: "bad credential"}}, nil
	})
	src.RetryBackoff = time.Millisecond

	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("a wrong password must fail immediately")
	}
	if calls != 1 {
		t.Fatalf("login attempts = %d, want 1 — retrying a rejected credential burns the rate limit "+
			"the retry exists to survive, and hides the real reason behind a timeout", calls)
	}
}

func TestLogin_GivesUpAndSaysWhy(t *testing.T) {
	src := NewLoginTokenSource(AuthSpec{
		Procedure: "svc/Login",
		Body:      func() ([]byte, error) { return []byte(`{}`), nil },
		TokenPath: "access_token",
	}, func(ctx context.Context, c *Call) (*Result, error) {
		return &Result{Status: 429, Error: &Error{Code: "resource_exhausted", Message: "rate limit exceeded, retry later"}}, nil
	})
	src.RetryBackoff = time.Millisecond

	_, err := src.Token(context.Background())
	if err == nil {
		t.Fatal("an endless refusal must eventually surface")
	}
	var ce *Error
	if !errors.As(err, &ce) && !contains(err.Error(), "resource_exhausted") {
		t.Fatalf("the final error must still name the refusal it gave up on, got %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
