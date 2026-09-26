package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func latencyServer(t *testing.T, slow func(n int) bool) (*httptest.Server, func() int) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if slow(n) {
			time.Sleep(60 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"code":"OK"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() int { mu.Lock(); defer mu.Unlock(); return calls }
}

func latencyRunner(t *testing.T, url string) *runner.Runner {
	cfg := testConfig(url)
	cfg.Auth = nil
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, Auth: deps.Bindings}
}

func suspectOver(ms int64) func(string, int64) bool {
	return func(_ string, got int64) bool { return got >= ms }
}

func TestASlowReadIsReMeasuredAndStopsAtTheFirstFastAnswer(t *testing.T) {
	srv, calls := latencyServer(t, func(n int) bool { return n == 1 })
	r := latencyRunner(t, srv.URL)
	c := normalized(t, &chain.Chain{Name: "slow", Steps: []*chain.Step{
		{ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "a"}, Expect: okExpect()},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{LatencySuspect: suspectOver(40), Remeasure: 2})
	if err != nil {
		t.Fatal(err)
	}
	st := rec.Steps[0]
	if st.LatencyMS < 50 {
		t.Fatalf("the first measurement is kept as the step's latency, got %dms", st.LatencyMS)
	}
	if len(st.LatencyResent) != 1 || st.LatencyResent[0] >= 40 || calls() != 2 {
		t.Fatalf("a slow read is re-sent until an answer is fast, at most twice: resent %v, calls %d", st.LatencyResent, calls())
	}
}

func TestASteadilySlowReadIsReMeasuredAsOftenAsAsked(t *testing.T) {
	srv, calls := latencyServer(t, func(int) bool { return true })
	r := latencyRunner(t, srv.URL)
	c := normalized(t, &chain.Chain{Name: "slow", Steps: []*chain.Step{
		{ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "a"}, Expect: okExpect()},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{LatencySuspect: suspectOver(40), Remeasure: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.Steps[0].LatencyResent; len(got) != 2 || calls() != 3 {
		t.Fatalf("want two re-measurements, got %v after %d calls", got, calls())
	}
}

func TestASlowWriteIsNeverReSent(t *testing.T) {
	srv, calls := latencyServer(t, func(int) bool { return true })
	r := latencyRunner(t, srv.URL)
	c := normalized(t, &chain.Chain{Name: "slow", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", SkipAuth: true, Body: map[string]any{"name": "w", "kind": "KIND_A"}, Expect: okExpect()},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{LatencySuspect: suspectOver(40), Remeasure: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.Steps[0].LatencyResent; len(got) != 0 || calls() != 1 {
		t.Fatalf("a write must be sent once: resent %v, calls %d", got, calls())
	}
}
