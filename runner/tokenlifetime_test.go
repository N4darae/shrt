package runner_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestATokenRefusedLongBeforeItsStatedExpiryIsRecordedWithItsAgeAndNotCalledARestart(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	login := expiringLoginServer(f)
	defer login.Close()
	expiring := login.Config.Handler
	creates := 0
	login.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Create") {
			creates++
			if creates == 2 {
				f.mu.Lock()
				f.tokens = map[string]bool{}
				f.mu.Unlock()
			}
		}
		expiring.ServeHTTP(w, r)
	})
	c := normalized(t, &chain.Chain{Name: "lifetime", Steps: []*chain.Step{
		{ID: "first", Call: "ThingService/Create", Body: map[string]any{"name": "a", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "second", Call: "ThingService/Create", Body: map[string]any{"name": "b", "kind": "KIND_A"}, Expect: okExpect()},
	}})
	rec, err := cachedRunner(t, login.URL, t.TempDir()).Run(context.Background(), c, runner.Options{KeepGoing: true})
	if err != nil {
		t.Fatal(err)
	}
	st := rec.Steps[1]
	if st.Status != runner.StatusError {
		t.Fatalf("the refused write is error: %s %s", st.Status, st.Error)
	}
	if len(st.TokenRefused) != 1 {
		t.Fatalf("the step must record the refused token's issue and expiry: %+v", st.TokenRefused)
	}
	r := st.TokenRefused[0]
	stated, ok := r.Stated()
	if !ok || stated < 59*time.Minute || stated > 61*time.Minute || !r.Early() || r.Cached || r.FirstUse {
		t.Fatalf("a token minted in this run, accepted, then refused an hour before its stated expiry: %+v", r)
	}
	if r.Token == "" || strings.Contains(r.Token, "token-") {
		t.Fatalf("the record carries a fingerprint of the token, never the token: %q", r.Token)
	}
	if strings.Contains(st.Error, "likely restarted") {
		t.Fatalf("nothing shows a restart, so the step must not call it one: %q", st.Error)
	}
	if !strings.Contains(st.Error, "after issue although the login said it expires in 3600s") {
		t.Fatalf("the step must say how long after issue the token was refused and what the login stated: %q", st.Error)
	}
}

func TestACachedTokenRefusedBeforeItsStatedExpiryOnItsFirstUseSaysHowOldItWas(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	srv := expiringLoginServer(f)
	defer srv.Close()
	root := t.TempDir()
	first, err := cachedRunner(t, srv.URL, root).Run(context.Background(), testChain(), runner.Options{})
	if err != nil || !first.Passed() {
		t.Fatalf("setup: the first run must pass: %v %s", err, first.Failure)
	}
	f.mu.Lock()
	f.tokens = map[string]bool{}
	f.mu.Unlock()
	rec, err := cachedRunner(t, srv.URL, root).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	st := rec.Steps[0]
	if !rec.Passed() || st.AuthRetry != runner.AuthRetryResent {
		t.Fatalf("setup: the cached token is refused, a fresh login made and the write re-sent: %s %s", rec.Status, st.Warning)
	}
	if len(st.TokenRefused) != 1 || !st.TokenRefused[0].Cached || !st.TokenRefused[0].FirstUse || !st.TokenRefused[0].Early() {
		t.Fatalf("the refusal of an untried cached token must be recorded as such: %+v", st.TokenRefused)
	}
	if _, ok := st.TokenRefused[0].Age(); !ok {
		t.Fatalf("the cache keeps when the token was issued: %+v", st.TokenRefused[0])
	}
	if !strings.Contains(st.Warning, "after issue although the login said it expires in 3600s") {
		t.Fatalf("the warning must say the token died before its stated expiry: %q", st.Warning)
	}
}
