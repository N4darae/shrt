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

const dropTwiceChain = `apiVersion: shrt/v1
name: cli-drop-twice
steps:
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-9}
      expect:
          - path: error.code
            equals: OK
    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect:
          - path: error.code
            equals: OK
    - id: fetch2
      call: ThingService/Fetch
      body: {id: thing-10}
      expect:
          - path: error.code
            equals: OK
`

func TestADroppedCallWhoseRPCAnsweredLaterInTheSameRunIsAFindingAboutThatStepOnceRepeated(t *testing.T) {
	var drop atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if drop.Load() && body["id"] == "thing-9" {
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
	writeFile(t, ".shrt/chains/cli-drop-twice.yaml", dropTwiceChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-drop-twice", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drop-twice", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drop-twice", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	drop.Store(true)
	var err error
	var coded *exitError
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-drop-twice", "-quiet"}) })
	if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out+err.Error(), "FINDING") {
		t.Fatalf("verify 1: the previous run had fetch answered, so one drop is could-not-verify, exit 3: %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "looks intermittent") {
		t.Fatalf("verify 1: the previous run had this step answered, so it looks intermittent: %v", err)
	}
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-drop-twice", "-quiet"}) })
	if err == nil || errors.As(err, &coded) || !strings.Contains(out, "FINDING: ") {
		t.Fatalf("verify 2: step fetch dropped in this run and the previous one while later steps were answered in both is a "+
			"finding about that step, exit 1, although ThingService/Fetch answered fetch2: %v\n%s", err, out)
	}
	if strings.Contains(err.Error(), "fails this rpc every time") || !strings.Contains(err.Error(), "step 1 fetch") ||
		!strings.Contains(err.Error(), "thing-9") {
		t.Fatalf("verify 2: the finding is worded by step and request, not the rpc every time: %v", err)
	}
}
