package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestATokenAcceptedEarlierThenRefusedReadsAsARestart(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	creates := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Create") {
			creates++
			if creates == 2 {
				f.mu.Lock()
				f.tokens = map[string]bool{}
				f.mu.Unlock()
			}
		}
		f.handle(w, r)
	}))
	defer srv.Close()
	c := &chain.Chain{Name: "restart", Steps: []*chain.Step{
		{ID: "first", Call: "ThingService/Create", Body: map[string]any{"name": "a", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "second", Call: "ThingService/Create", Body: map[string]any{"name": "b", "kind": "KIND_A"}, Expect: okExpect()},
	}}
	c = normalized(t, c)
	rec, err := lateRunner(t, srv.URL).Run(context.Background(), c, runner.Options{KeepGoing: true})
	if err != nil {
		t.Fatal(err)
	}
	st := rec.Steps[1]
	if st.Status != runner.StatusError {
		t.Fatalf("the refused step is error: %s %s", st.Status, st.Error)
	}
	if strings.Contains(st.Error, "auth regression") || runner.RefusedFreshToken(st) {
		t.Fatalf("a token the backend accepted earlier in this run and then refused points at a restart, not an auth regression: %q", st.Error)
	}
	if !strings.Contains(st.Error, "restarted mid-run") {
		t.Fatalf("the step must say the backend likely restarted mid-run: %q", st.Error)
	}
}
