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

func TestADroppedCallWhoseRPCAnsweredLaterInTheSameRunIsNotAFinding(t *testing.T) {
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
	for i := range 2 {
		out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-drop-twice", "-quiet"}) })
		if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out+err.Error(), "FINDING") ||
			strings.Contains(err.Error(), "fails this rpc every time") {
			t.Fatalf("verify %d: ThingService/Fetch answered step fetch2 in the same run, so the rpc does not fail every time; "+
				"the drop stays could-not-verify, exit 3: %v\n%s", i+1, err, out)
		}
	}
}
