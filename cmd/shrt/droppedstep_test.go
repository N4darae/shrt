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

const dropOneRequestChain = `apiVersion: shrt/v1
name: cli-drop-one
steps:
    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect:
          - path: error.code
            equals: OK
    - id: fetch_a
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - path: error.code
            equals: OK
    - id: fetch_b
      call: ThingService/Fetch
      body: {id: thing-9}
      expect:
          - path: error.code
            equals: OK
    - id: fetch_c
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - path: error.code
            equals: OK
`

func TestAStepDroppedEveryRunWhileItsRPCAnswersOtherStepsIsAFinding(t *testing.T) {
	var drop atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if drop.Load() && r.URL.Path == "/shrt.test.v1.ThingService/Fetch" && body["id"] == "thing-9" {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-drop-one.yaml", dropOneRequestChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-drop-one", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drop-one", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drop-one", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	drop.Store(true)
	var err error
	var coded *exitError
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-drop-one", "-quiet"}) })
	if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out, "FINDING") {
		t.Fatalf("the previous run answered fetch_b, so a first drop is could-not-verify, exit 3: %v\n%s", err, out)
	}
	for _, wrong := range []string{"check the backend is up", "stopped or crashed"} {
		if strings.Contains(err.Error(), wrong) {
			t.Fatalf("a later step was answered, so the backend did not stop; %q is wrong: %v", wrong, err)
		}
	}
	if !strings.Contains(err.Error(), "looks intermittent") || !strings.Contains(err.Error(), "fetch_b") {
		t.Fatalf("the previous run answered this step, so it looks intermittent, said of the step: %v", err)
	}
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-drop-one", "-quiet"}) })
	if err == nil || errors.As(err, &coded) || !strings.Contains(out, "FINDING: ") {
		t.Fatalf("the same step dropped in two runs while later steps were answered in both is a finding, exit 1, even though its rpc answered other steps: %v\n%s", err, out)
	}
	for _, want := range []string{"fetch_b", "thing-9", "fetch_a"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the finding names the step, its request and the steps its rpc answered (%q): %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "fails this rpc every time") {
		t.Fatalf("the rpc answered other steps, so the finding is about this step, not the rpc every time: %v", err)
	}
}
