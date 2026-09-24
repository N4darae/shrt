package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestANotAsPinnedRunPrintsEachUnpinnedFailureOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "gadget"})
		}
	}))
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, filepath.Join(".shrt", "chains", "red.yaml"), `name: red
kept_red:
    - {step: create, path: id, got: thing-1}
steps:
    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A, idempotency_key: "${uuid}"}
      expect:
          - {path: id, equals: thing-9}
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - {path: name, equals: widget}
`)
	var err error
	out := captureStdout(t, func() {
		err = runRun(context.Background(), []string{"red"})
	})
	if err == nil {
		t.Fatalf("a failure outside the pin is not as pinned:\n%s", out)
	}
	if n := strings.Count(out, "want=widget got=gadget"); n != 1 {
		t.Fatalf("with the step lines shown, the unpinned failure is printed once, got %d times:\n%s", n, out)
	}
	if !strings.Contains(out, "NEW FAILURE outside the pinned defect: fetch") {
		t.Errorf("the new-failure line still names the step:\n%s", out)
	}
	out = captureStdout(t, func() {
		err = runRun(context.Background(), []string{"red", "-quiet"})
	})
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "kept red (not_as_pinned)") && strings.Contains(line, "got=gadget") {
			t.Fatalf("the kept red line names the step and path and leaves the values to the NEW FAILURE line:\n%s", out)
		}
	}
	if !strings.Contains(out, "NEW FAILURE outside the pinned defect: fetch name want=widget got=gadget") {
		t.Fatalf("under -quiet no step line was printed, so the NEW FAILURE line carries the values:\n%s", out)
	}
}
