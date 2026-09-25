package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newLeakyRefusalBackend() *httptest.Server {
	next := 0
	totals := map[string]int{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			if meta, _ := body["meta"].(map[string]any); meta != nil {
				if id, _ := meta["trace_id"].(string); id != "" {
					totals[id]--
					_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "REJECTED"}})
					return
				}
			}
			next++
			id := "thing-" + itoa(next)
			totals[id] = 5
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id})
		case "/shrt.test.v1.ThingService/Fetch":
			id, _ := body["id"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id, "total": totals[id]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func leakyRefusalChain(takeExpects string) string {
	return `apiVersion: shrt/v1
name: cli-leaky
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: other
      call: ThingService/Create
      body:
          name: other
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: take
      call: ThingService/Create
      body:
          name: take
          kind: KIND_A
          idempotency_key: ${uuid}
          meta:
              trace_id: ${create.id}
      expect:
          - path: error.code
            equals: ` + takeExpects + `
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: total
            equals: 5
`
}

func leakySlice(t *testing.T, takeExpects string, args ...string) (string, error) {
	t.Helper()
	srv := newLeakyRefusalBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-leaky.yaml", leakyRefusalChain(takeExpects))
	_ = runRun(context.Background(), []string{"cli-leaky", "-quiet", "-keep-going"})
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), append([]string{"cli-leaky", "-step", "fetch", "-run", "latest", "-verify", "-v"}, args...))
	})
	return out, err
}

func TestCLISliceKeepsARefusedWriteOnTheEntityTheTargetReads(t *testing.T) {
	out, err := leakySlice(t, "REJECTED")
	if err != nil || !strings.Contains(out, "verify reproduced") {
		t.Fatalf("take was refused yet moved the total fetch reads, so the slice keeps it and reproduces (err %v):\n%s", err, out)
	}
	if !strings.Contains(out, "take") || strings.Contains(out, "act on no entity") {
		t.Fatalf("take acts on what create made, so it is kept, not dropped as refused:\n%s", out)
	}
}

func TestCLISliceKeepWritesKeepsARefusedWrite(t *testing.T) {
	out, err := leakySlice(t, "REJECTED", "-keep", "writes")
	if err != nil || !strings.Contains(out, "take") || !strings.Contains(out, "kept by -keep writes") {
		t.Fatalf("-keep writes keeps every earlier write, refused or not (err %v):\n%s", err, out)
	}
}

func TestCLISliceRelaxesAKeptWriteRefusedAgainstItsExpectation(t *testing.T) {
	out, err := leakySlice(t, "OK")
	if err != nil || !strings.Contains(out, "verify reproduced") || !strings.Contains(out, "take error.code equals") {
		t.Fatalf("take was answered with a refusal it did not expect, and still moved the total: the slice keeps it with that expectation relaxed (err %v):\n%s", err, out)
	}
}
