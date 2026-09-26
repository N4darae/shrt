package runner_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestACachedTokenRefusedAndThenAFreshOneIsNotBlamedOnTheCache(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	srv := expiringLoginServer(f)
	defer srv.Close()
	expiring := srv.Config.Handler
	var refuse atomic.Bool
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if refuse.Load() && strings.HasSuffix(r.URL.Path, "/Create") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"unauthenticated","message":"token rejected"}`))
			return
		}
		expiring.ServeHTTP(w, r)
	})
	root := t.TempDir()
	first, err := cachedRunner(t, srv.URL, root).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Passed() {
		t.Fatalf("setup: the first run must pass: %s", first.Failure)
	}
	refuse.Store(true)
	rec, err := cachedRunner(t, srv.URL, root).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	st := rec.Steps[0]
	if st.AuthRetry != runner.AuthRetryResent || !runner.RefusedFreshToken(st) {
		t.Fatalf("setup: the cached token is refused, a fresh login made, and the fresh token refused too: retry=%q error=%q", st.AuthRetry, st.Error)
	}
	if strings.Contains(st.Warning, "a restart or a revoke") {
		t.Fatalf("the freshly issued token was refused too, so the refusal is not about the cached token: %q", st.Warning)
	}
}
