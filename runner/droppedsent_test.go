package runner_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAConnectionDroppedAfterTheRequestWasWrittenIsSentWithNoAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Auth = nil
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, Auth: deps.Bindings}
	c := normalized(t, &chain.Chain{Name: "dropped", Steps: []*chain.Step{
		{ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "a"}, Expect: okExpect()},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	msg := rec.Steps[0].Error
	if !strings.Contains(msg, "sent, no answer") || strings.HasPrefix(msg, "not sent") {
		t.Fatalf("the backend read the whole request before closing the connection, so it was sent and got no answer, got %q", msg)
	}
	if !strings.Contains(msg, "whether the call took effect is unknown") {
		t.Fatalf("a sent call with no answer may have taken effect, got %q", msg)
	}
}
