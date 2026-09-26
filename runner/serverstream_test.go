package runner_test

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func streamFrame(flags byte, payload string) []byte {
	out := make([]byte, 5, 5+len(payload))
	out[0] = flags
	binary.BigEndian.PutUint32(out[1:], uint32(len(payload)))
	return append(out, payload...)
}

func TestAServerStreamingStepAssertsItsFirstMessageAndItsRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/connect+json")
		if r.Header.Get("X-Deny") != "" {
			_, _ = w.Write(streamFrame(2, `{"error":{"code":"unauthenticated","message":"no token"}}`))
			return
		}
		_, _ = w.Write(streamFrame(0, `{"idOrder":"o-1","state":"OPEN"}`))
		_, _ = w.Write(streamFrame(2, `{}`))
	}))
	defer srv.Close()
	cfg := config.Default()
	cfg.Target.BaseURL = srv.URL
	deps, err := runner.Build(context.Background(), cfg, catalogtest.Rich(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client}
	c := normalized(t, &chain.Chain{Name: "watch", Steps: []*chain.Step{
		{ID: "watch", Call: "OrderService/WatchOrder", Body: map[string]any{"id_order": "o-1"},
			Expect: []chain.Expectation{{Path: "messages.0.state", Equals: "OPEN"}, {Path: "messages.1", Exists: ptrBool(false)}}},
		{ID: "denied", Call: "OrderService/WatchOrder", Body: map[string]any{"id_order": "o-1"}, Headers: map[string]string{"X-Deny": "1"},
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "unauthenticated"}}},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusPassed {
		t.Fatalf("both steps must pass: %s\n%s", rec.Status, rec.Failure)
	}
}

func ptrBool(b bool) *bool { return &b }
