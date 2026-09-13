package transport

import (
	"context"
	"testing"
	"time"
)

func TestWithRetryStopsOnFirstNonRetryableResult(t *testing.T) {
	calls := 0
	h := WithRetry(5, time.Millisecond, func(*Result, error) bool { return false })(
		func(ctx context.Context, call *Call) (*Result, error) {
			calls++
			return &Result{Status: 200}, nil
		})
	if _, err := h(context.Background(), &Call{}); err != nil {
		t.Fatalf("h: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 — a non-retryable result must not be retried", calls)
	}
}

func TestWithRetryRetriesUntilNotRetryable(t *testing.T) {
	calls := 0
	h := WithRetry(5, time.Millisecond, func(res *Result, err error) bool {
		return res != nil && res.Status == 503
	})(func(ctx context.Context, call *Call) (*Result, error) {
		calls++
		if calls < 3 {
			return &Result{Status: 503}, nil
		}
		return &Result{Status: 200}, nil
	})
	res, err := h(context.Background(), &Call{})
	if err != nil {
		t.Fatalf("h: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if res.Status != 200 {
		t.Fatalf("final result status = %d, want 200", res.Status)
	}
}

func TestWithRetryGivesUpAfterAttempts(t *testing.T) {
	calls := 0
	h := WithRetry(3, time.Millisecond, func(*Result, error) bool { return true })(
		func(ctx context.Context, call *Call) (*Result, error) {
			calls++
			return &Result{Status: 503}, nil
		})
	res, err := h(context.Background(), &Call{})
	if err != nil {
		t.Fatalf("h: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 — must stop at the attempt budget, not loop forever", calls)
	}
	if res.Status != 503 {
		t.Fatalf("final result status = %d, want the last attempt's result", res.Status)
	}
}

func TestWithRetryClampsAttemptsBelowOne(t *testing.T) {
	calls := 0
	h := WithRetry(0, time.Millisecond, func(*Result, error) bool { return true })(
		func(ctx context.Context, call *Call) (*Result, error) {
			calls++
			return &Result{Status: 503}, nil
		})
	if _, err := h(context.Background(), &Call{}); err != nil {
		t.Fatalf("h: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 — attempts < 1 must still make one attempt", calls)
	}
}

func TestWithRetryStopsOnContextCancellation(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	h := WithRetry(5, 50*time.Millisecond, func(*Result, error) bool { return true })(
		func(ctx context.Context, call *Call) (*Result, error) {
			calls++
			if calls == 1 {
				cancel()
			}
			return &Result{Status: 503}, nil
		})
	_, err := h(ctx, &Call{})
	if err == nil {
		t.Fatal("a cancelled context must surface as an error, not a silent success")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 — cancellation during backoff must stop further attempts", calls)
	}
}

type fakeTokenSource struct {
	token        string
	invalidated  int
	tokenAtCalls []int
	calls        int
}

func (f *fakeTokenSource) Token(ctx context.Context) (string, error) {
	f.calls++
	f.tokenAtCalls = append(f.tokenAtCalls, f.calls)
	return f.token, nil
}

func (f *fakeTokenSource) Invalidate() { f.invalidated++ }

func TestWithAuthAttachesTokenHeader(t *testing.T) {
	src := &fakeTokenSource{token: "tok-1"}
	var seenHeader string
	h := WithAuth(src, AuthSpec{}, nil)(func(ctx context.Context, call *Call) (*Result, error) {
		seenHeader = call.Header.Get("Authorization")
		return &Result{Status: 200}, nil
	})
	if _, err := h(context.Background(), &Call{}); err != nil {
		t.Fatalf("h: %v", err)
	}
	if seenHeader != "Bearer tok-1" {
		t.Fatalf("Authorization header = %q, want %q", seenHeader, "Bearer tok-1")
	}
}

func TestWithAuthInvalidatesAndRetriesOnUnauthenticated(t *testing.T) {
	src := &fakeTokenSource{token: "tok-1"}
	attempt := 0
	h := WithAuth(src, AuthSpec{}, nil)(func(ctx context.Context, call *Call) (*Result, error) {
		attempt++
		if attempt == 1 {
			return &Result{Status: 401, Error: &Error{Code: "unauthenticated"}}, nil
		}
		return &Result{Status: 200}, nil
	})
	res, err := h(context.Background(), &Call{})
	if err != nil {
		t.Fatalf("h: %v", err)
	}
	if res.Status != 200 {
		t.Fatalf("final status = %d, want 200 after re-auth", res.Status)
	}
	if src.invalidated != 1 {
		t.Fatalf("invalidated = %d, want 1", src.invalidated)
	}
	if src.calls != 2 {
		t.Fatalf("Token() was called %d time(s), want 2 — once before the 401, once after invalidating", src.calls)
	}
}

func TestAuthRouterResolveNamesTheKnownProfilesWhenOneIsMissing(t *testing.T) {
	router := AuthRouter{Profiles: []*AuthProfile{{Name: "default"}, {Name: "partner"}}, Default: "default"}
	_, err := router.Resolve(&Call{Meta: map[string]any{"auth": "checker"}})
	if err == nil {
		t.Fatal("resolving an undeclared profile must fail")
	}
	if !contains(err.Error(), "default") || !contains(err.Error(), "partner") {
		t.Fatalf("error must list the declared profiles so the author can fix the typo, got: %v", err)
	}
}

func TestLoginTokenSourceLoginsCountsSuccessfulLogins(t *testing.T) {
	src := NewLoginTokenSource(AuthSpec{
		Procedure: "svc/Login",
		Body:      func() ([]byte, error) { return []byte(`{}`), nil },
		TokenPath: "access_token",
	}, func(ctx context.Context, c *Call) (*Result, error) {
		return &Result{Status: 200, Body: []byte(`{"access_token":"tok"}`)}, nil
	})
	if src.Logins() != 0 {
		t.Fatalf("Logins() = %d before any login, want 0", src.Logins())
	}
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}
	if src.Logins() != 1 {
		t.Fatalf("Logins() = %d after one login, want 1", src.Logins())
	}
}

func TestWithObserverReportsProcedureStatusAndLatency(t *testing.T) {
	var got Event
	h := WithObserver(func(ev Event) { got = ev })(func(ctx context.Context, call *Call) (*Result, error) {
		return &Result{Status: 200, Latency: 5 * time.Millisecond}, nil
	})
	if _, err := h(context.Background(), &Call{Procedure: "Svc/Method"}); err != nil {
		t.Fatalf("h: %v", err)
	}
	if got.Procedure != "Svc/Method" || got.Status != 200 || got.Latency != 5*time.Millisecond {
		t.Fatalf("observed event = %+v, want procedure/status/latency carried through from the result", got)
	}
}

func TestWithAuthSkipsWhenRequested(t *testing.T) {
	src := &fakeTokenSource{token: "tok-1"}
	h := WithAuth(src, AuthSpec{}, func(procedure string) bool { return true })(
		func(ctx context.Context, call *Call) (*Result, error) {
			return &Result{Status: 200}, nil
		})
	if _, err := h(context.Background(), &Call{Procedure: "Svc/Method"}); err != nil {
		t.Fatalf("h: %v", err)
	}
	if src.calls != 0 {
		t.Fatalf("Token() was called %d time(s), want 0 — skip must bypass the source entirely", src.calls)
	}
}
