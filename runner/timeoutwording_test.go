package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestATimedOutCallIsRecordedAsSentWithTheTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"code":"OK"},"id":"x"}`))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Auth = nil
	cfg.Target.Timeout = "50ms"
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
	msg := rec.Steps[0].Error
	if !strings.Contains(msg, "sent, no answer before target.timeout (50ms)") {
		t.Fatalf("a call that went out and timed out must say so, with the timeout, got %q", msg)
	}
}
