package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const driftSkippedWriteChain = `apiVersion: shrt/v1
name: cli-drift-skipped-write
steps:
    - id: make
      call: ThingService/Create
      body:
          name: widget
      expect:
          - path: error.code
            equals: OK
    - id: confirm
      call: ThingService/Create
      body:
          name: confirm-${make.id}
      expect:
          - path: error.code
            equals: OK
    - id: stock
      call: ThingService/Fetch
      body:
          id: stock
      expect:
          - path: total
            equals: 7
`

func TestDescriptorDriftBeforeASkippedWriteIsNoRegression(t *testing.T) {
	broken := false
	qty := 10
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			out := map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": body["name"]}
			if body["name"] == "widget" {
				qty = 10
				if broken {
					out["traceHint"] = "t-1"
				}
			} else {
				qty -= 3
			}
			_ = json.NewEncoder(w).Encode(out)
		case "/shrt.test.v1.ThingService/Fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "total": qty})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	raw, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", string(raw)+"conventions:\n    validate_output: true\n")
	writeFile(t, ".shrt/chains/cli-drift-skipped-write.yaml", driftSkippedWriteChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-drift-skipped-write", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drift-skipped-write", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drift-skipped-write", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	broken = true
	var verr error
	out := captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-drift-skipped-write", "-quiet"}) })
	if code := exitCodeOf(verr); code != 3 {
		t.Fatalf("the write confirm was not sent because make drifted, so stock's change is its side effect, not evidence: want exit 3, got %d: %v\n%s", code, verr, out)
	}
	if strings.Contains(verr.Error(), "regression") {
		t.Fatalf("no regression verdict after a skipped write: %v", verr)
	}
	if !strings.Contains(verr.Error(), "confirm") {
		t.Fatalf("the message names the write that was not sent: %v", verr)
	}
}
