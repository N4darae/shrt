package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newSlowFetchBackend(delay *atomic.Int64) *httptest.Server {
	var next atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			n := next.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(int(n))})
		case "/shrt.test.v1.ThingService/Fetch":
			time.Sleep(time.Duration(delay.Load()) * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget",
				"created_at": "2026-09-01T10:00:00Z",
			})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestVerifyWarnsOfALatencyRegressionAndFailsOnlyWhenConfigured(t *testing.T) {
	var delay atomic.Int64
	srv := newSlowFetchBackend(&delay)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "fetch returns the created name"}); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	cfg, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", string(cfg)+"latency:\n    floor_ms: 100\n")
	delay.Store(150)
	var verr error
	out := captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-latency"}) })
	if verr != nil {
		t.Fatalf("a latency regression is a warning by default: %v\n%s", verr, out)
	}
	if !strings.Contains(out, "LATENCY: Fetch at step fetch") || !strings.Contains(out, "re-sent 2 more time(s)") {
		t.Fatalf("verify names the slow rpc and step, re-measured:\n%s", out)
	}
	if !strings.Contains(out, "latency per step") {
		t.Fatalf("-latency lists every step:\n%s", out)
	}
	writeFile(t, ".shrt/config.yaml", string(cfg)+"latency:\n    floor_ms: 100\n    fail: true\n")
	out = captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if exitCodeOf(verr) != 1 || !strings.Contains(verr.Error(), "latency regression") {
		t.Fatalf("latency.fail makes a confirmed slowdown fail verify, got %v\n%s", verr, out)
	}
	delay.Store(0)
	out = captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if verr != nil || strings.Contains(out, "LATENCY") {
		t.Fatalf("no slowdown, no latency line: %v\n%s", verr, out)
	}
}
