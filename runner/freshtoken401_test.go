package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func alwaysUnauthServer(f *fakeServer, rpc string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/"+rpc) {
			f.handle(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthenticated","message":"token rejected"}`))
	}))
}

func TestAFreshTokenTheBackendRefusesIsCalledAPossibleAuthRegression(t *testing.T) {
	for _, tc := range []struct{ rpc, step, evidence string }{
		{"Create", "create", "a login in this run had just issued"},
		{"Fetch", "fetch", "re-sent with the new token"},
	} {
		t.Run(tc.rpc, func(t *testing.T) {
			f := newFakeServer()
			defer f.Close()
			srv := alwaysUnauthServer(f, tc.rpc)
			defer srv.Close()
			rec, err := lateRunner(t, srv.URL).Run(context.Background(), testChain(), runner.Options{KeepGoing: true})
			if err != nil {
				t.Fatal(err)
			}
			var st *runner.StepRecord
			for _, s := range rec.Steps {
				if s.ID == tc.step {
					st = s
				}
			}
			if st == nil || st.Status != runner.StatusError {
				t.Fatalf("a refused step stays error: %+v", st)
			}
			if !strings.Contains(st.Error, "auth regression") || !strings.Contains(st.Error, tc.evidence) || !runner.RefusedFreshToken(st) {
				t.Fatalf("the backend refused a token a login in this run just issued; the step must say it may be an auth regression, with the evidence %q: %q", tc.evidence, st.Error)
			}
			if strings.Contains(st.Error, "check the credentials") {
				t.Fatalf("the credentials worked, so the step must not blame them: %q", st.Error)
			}
		})
	}
}
