package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAnUnavailableAnswerIsNotAVerdictAboutTheService(t *testing.T) {
	cases := []struct {
		name   string
		status int
		ctype  string
		body   string
	}{
		{"connect unavailable", http.StatusServiceUnavailable, "application/json", `{"code":"unavailable","message":"upstream connect error"}`},
		{"bare 502", http.StatusBadGateway, "text/html", `<html>Bad Gateway</html>`},
		{"bare 504", http.StatusGatewayTimeout, "text/plain", `gateway timeout`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.ctype)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			cfg := testConfig(srv.URL)
			cfg.Auth = nil
			deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
			if err != nil {
				t.Fatal(err)
			}
			r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, Auth: deps.Bindings}
			c := normalized(t, &chain.Chain{Name: "gw", Steps: []*chain.Step{
				{ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "a"}, Expect: okExpect()},
			}})
			rec, err := r.Run(context.Background(), c, runner.Options{})
			if err != nil {
				t.Fatal(err)
			}
			st := rec.Steps[0]
			if st.Status != runner.StatusError || rec.Status != runner.StatusError {
				t.Fatalf("a gateway's unavailable answer is not the service's verdict: step %s, run %s", st.Status, rec.Status)
			}
			if !runner.NotAnsweredByService(st) {
				t.Fatal("the step must be recognisable as not answered by the service")
			}
			if !strings.Contains(st.Error, "not answered by the service") {
				t.Fatalf("the error must say the service did not answer, got %q", st.Error)
			}
		})
	}
}

func TestAnAssertedUnavailableStillPasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"unavailable","message":"draining"}`))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Auth = nil
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, Auth: deps.Bindings}
	c := normalized(t, &chain.Chain{Name: "gw", Steps: []*chain.Step{
		{ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "a"},
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "unavailable"}}},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Steps[0].Status != runner.StatusPassed {
		t.Fatalf("a step asserting unavailable passes when it gets it: %s %s", rec.Steps[0].Status, rec.Steps[0].Error)
	}
}
