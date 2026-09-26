package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAReadWithAServerErrorIsResentOnceAndAWriteNever(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		n := calls[r.URL.Path]
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":"internal","message":"pool exhausted"}`))
			return
		}
		_, _ = w.Write([]byte(`{"error":{"code":"OK"},"id":"a","name":"widget"}`))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Auth = nil
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, Auth: deps.Bindings}
	c := normalized(t, &chain.Chain{Name: "resend", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", SkipAuth: true, Body: map[string]any{"name": "widget"}, AllowFail: true},
		{ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "a"}, Expect: okExpect(),
			Export: map[string]string{"got": "name"}},
		{ID: "echo", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "${got}"}, Expect: okExpect()},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	create, fetch, echo := rec.Steps[0], rec.Steps[1], rec.Steps[2]
	if create.FirstAttempt != nil || create.Transport == nil || calls["/shrt.test.v1.ThingService/Create"] != 1 {
		t.Fatalf("a write is never re-sent: %+v, %d calls", create, calls["/shrt.test.v1.ThingService/Create"])
	}
	if fetch.Status != runner.StatusPassed || fetch.FirstAttempt == nil || fetch.FirstAttempt.Code != "internal" ||
		fetch.FirstAttempt.HTTPStatus != 500 || !strings.Contains(fetch.Warning, "re-sent once") {
		t.Fatalf("a read's server error is re-sent once, both attempts recorded and the step judged on the answer: %+v", fetch)
	}
	if echo.Status != runner.StatusPassed || calls["/shrt.test.v1.ThingService/Fetch"] != 3 {
		t.Fatalf("a step reading the re-sent answer runs on it: %s, %d fetch calls", echo.Status, calls["/shrt.test.v1.ThingService/Fetch"])
	}
}

func TestAReadAssertingItsTransportCodeIsNotResent(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"internal","message":"boom"}`))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Auth = nil
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, Auth: deps.Bindings}
	c := normalized(t, &chain.Chain{Name: "asserted", Steps: []*chain.Step{
		{ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "a"},
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "internal"}}},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || rec.Steps[0].FirstAttempt != nil || rec.Steps[0].Status != runner.StatusPassed {
		t.Fatalf("an asserted server error is the answer, not re-sent: %d calls, %+v", n, rec.Steps[0])
	}
}
