package runner_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestAWriteRefusedInBandWithAnUntriedCachedTokenIsNotSentTwice(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	srv := expiringLoginServer(f)
	defer srv.Close()
	expiring := srv.Config.Handler
	var inBand atomic.Bool
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inBand.Load() && strings.HasSuffix(r.URL.Path, "/Create") {
			expiring.ServeHTTP(&discardWriter{h: http.Header{}}, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"session expired"}}`))
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
	f.mu.Lock()
	creates := f.creates
	f.mu.Unlock()
	inBand.Store(true)
	rec, err := cachedRunner(t, srv.URL, root).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	got := f.creates
	f.mu.Unlock()
	if got != creates+1 {
		t.Fatalf("an HTTP 200 carrying an in-band unauthenticated means the handler ran, so the write must not be re-sent "+
			"(creates %d -> %d)", creates, got)
	}
	st := rec.Steps[0]
	if st.AuthRetry != runner.AuthRetryNotResent || !strings.Contains(st.Warning, "not re-sent") {
		t.Fatalf("the step must record auth_retry not_resent with a warning: auth_retry=%q warning=%q", st.AuthRetry, st.Warning)
	}
}
