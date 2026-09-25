package runner_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestACachedTokenAcceptedThenRefusedReadsAsARestartNotCredentials(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	login := expiringLoginServer(f)
	defer login.Close()
	expiring := login.Config.Handler
	root := t.TempDir()
	first, err := cachedRunner(t, login.URL, root).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Passed() {
		t.Fatalf("setup: the first run must pass: %s", first.Failure)
	}
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
	c := normalized(t, &chain.Chain{Name: "restart", Steps: []*chain.Step{
		{ID: "first", Call: "ThingService/Create", Body: map[string]any{"name": "a", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "second", Call: "ThingService/Create", Body: map[string]any{"name": "b", "kind": "KIND_A"}, Expect: okExpect()},
	}})
	rec, err := cachedRunner(t, login.URL, root).Run(context.Background(), c, runner.Options{KeepGoing: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Steps[0].AuthRetry != "" || rec.Steps[0].Status != runner.StatusPassed {
		t.Fatalf("setup: the first step must pass with the cached token: %s %s", rec.Steps[0].Status, rec.Steps[0].Warning)
	}
	st := rec.Steps[1]
	if st.Status != runner.StatusError {
		t.Fatalf("the refused step is error: %s %s", st.Status, st.Error)
	}
	if strings.Contains(st.Error, "check the credentials") {
		t.Fatalf("the backend accepted this cached token earlier in the run, so the credentials are not the cause: %q", st.Error)
	}
	if !strings.Contains(st.Error, "restarted") {
		t.Fatalf("the step must name a restart as an explanation: %q", st.Error)
	}
}
