package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const dropChain = `apiVersion: shrt/v1
name: cli-drop
steps:
    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-9}
      expect:
          - path: error.code
            equals: OK
    - id: again
      call: ThingService/Create
      body: {name: other, kind: KIND_A}
      expect:
          - path: error.code
            equals: OK
`

func TestAnRPCDroppedEveryRunWhileLaterStepsAreAnsweredIsAFinding(t *testing.T) {
	var drop, dropAll atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if drop.Load() && (r.URL.Path == "/shrt.test.v1.ThingService/Fetch" || dropAll.Load()) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-9", "name": "widget"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-drop.yaml", dropChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-drop", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drop", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drop", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	drop.Store(true)
	var err error
	var coded *exitError
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-drop", "-quiet"}) })
	if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out, "FINDING") {
		t.Fatalf("a first dropped connection is could-not-verify, exit 3: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-drop", "-quiet"}) })
	if err == nil || errors.As(err, &coded) || !strings.Contains(out, "FINDING: ") {
		t.Fatalf("the same rpc dropped in two runs while a later step was answered in both is a finding, exit 1: %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "ThingService/Fetch") || !strings.Contains(err.Error(), "fails this rpc every time while answering others") {
		t.Fatalf("the finding names the rpc: %v", err)
	}

	dropAll.Store(true)
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-drop", "-quiet"}) })
	if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out, "FINDING") {
		t.Fatalf("nothing after the dropped step was answered this run, so it may be down: exit 3: %v\n%s", err, out)
	}
}
