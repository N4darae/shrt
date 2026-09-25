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

const driftIndependentChain = `apiVersion: shrt/v1
name: cli-drift-independent
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
    - id: fetch_again
      call: ThingService/Fetch
      body:
          id: ${fetch.id}
      expect:
          - path: error.code
            equals: OK
    - id: other
      call: ThingService/Create
      body:
          name: gadget
      expect:
          - path: error.code
            equals: OK
`

func TestDescriptorDriftLeavesAnIndependentLaterStepJudged(t *testing.T) {
	broken := false
	nextID := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			nextID++
			name := body["name"]
			if broken && name == "gadget" {
				name = "gizmo"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(nextID), "name": name})
		case "/shrt.test.v1.ThingService/Fetch":
			out := map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget"}
			if broken {
				out["traceHint"] = "t-1"
			}
			_ = json.NewEncoder(w).Encode(out)
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
	writeFile(t, ".shrt/chains/cli-drift-independent.yaml", driftIndependentChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-drift-independent", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drift-independent", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drift-independent", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	broken = true
	var verr error
	out := captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-drift-independent", "-quiet"}) })
	if code := exitCodeOf(verr); code != 1 || !strings.Contains(verr.Error(), "regression") {
		t.Fatalf("step other reads nothing from the drifted fetch, so its change is judged: want a regression, exit 1, got %d: %v\n%s", code, verr, out)
	}
	if !strings.Contains(verr.Error(), "other name") || strings.Contains(verr.Error(), "fetch_again") {
		t.Fatalf("the verdict names the independent change, not the step reading the drifted one: %v", verr)
	}
}
