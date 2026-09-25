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

const unjudgedFoldChain = `apiVersion: shrt/v1
name: cli-unjudged
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
      expect:
          - path: error.code
            equals: OK
    - id: fetch_other
      call: ThingService/Fetch
      body:
          id: thing-fixed
      expect:
          - path: error.code
            equals: OK
`

func TestVerifyFoldsTheChangesAtAStepNotJudgedForDescriptorDrift(t *testing.T) {
	drift := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			out := map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "widget", "total": 3}
			if drift {
				out["total"] = "three"
				out["name"] = "gizmo"
			}
			_ = json.NewEncoder(w).Encode(out)
		case "/shrt.test.v1.ThingService/Fetch":
			name := "widget"
			if drift {
				name = "gadget"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-fixed", "name": name})
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
	writeFile(t, ".shrt/chains/cli-unjudged.yaml", unjudgedFoldChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-unjudged", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-unjudged", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-unjudged", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	drift = true
	verify := func(args ...string) string {
		var verr error
		out := captureStdout(t, func() { verr = runVerify(ctx, append([]string{"cli-unjudged", "-quiet"}, args...)) })
		if verr == nil {
			t.Fatalf("fetch_other changed and reads nothing from create, so verify fails:\n%s", out)
		}
		return out
	}
	out := verify()
	if !strings.Contains(out, "[create] not_judged") || !strings.Contains(out, "shrt catalog build") {
		t.Fatalf("the changes at a step not judged for descriptor drift fold into one line with the rebuild hint:\n%s", out)
	}
	if strings.Contains(out, "[create] changed") || strings.Contains(out, "[create] status") {
		t.Fatalf("no per-field line for a step that is not judged:\n%s", out)
	}
	if !strings.Contains(out, "[fetch_other] changed    name") {
		t.Fatalf("a change at a step that reads nothing from the drifted one is still listed:\n%s", out)
	}
	if out := verify("-v"); !strings.Contains(out, "[create] changed") {
		t.Fatalf("-v lists every change, also at a step not judged:\n%s", out)
	}
}
