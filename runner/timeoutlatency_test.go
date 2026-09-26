package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestATimedOutCallRecordsTheTimeItWaited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Auth = nil
	cfg.Target.Timeout = "80ms"
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, Auth: deps.Bindings}
	c := normalized(t, &chain.Chain{Name: "slow", Steps: []*chain.Step{
		{ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "a"}, Expect: okExpect()},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.Steps[0].LatencyMS; got < 70 {
		t.Fatalf("the step waited for target.timeout (80ms) before giving up; its latency must say so, got %dms", got)
	}
}
