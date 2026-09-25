package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const unnamedConflictChain = `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: n-${uuid}
          meta:
              source: fixed-source-ao3
      expect:
          - path: error.code
            equals: OK
`

func TestAConflictNamingNoFieldIsNotBlamedOnAUUIDField(t *testing.T) {
	var created atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if created.Add(1) > 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "REJECTED", "message": "duplicate record"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "n"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", unnamedConflictChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	for i := range 2 {
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet"}) })
		if err == nil {
			t.Fatalf("verify %d: a refused create does not verify clean\n%s", i+1, out)
		}
		text := out + err.Error()
		if strings.Contains(text, "on a field built from ${uuid}") || strings.Contains(text, "FINDING") {
			t.Fatalf("verify %d: the refusal names no field, and a ${uuid} value is unique to its run, so it is not what collided: %v\n%s", i+1, err, out)
		}
		if !strings.Contains(text, "CHAIN DEFECT") || !strings.Contains(err.Error(), "meta.source is the literal fixed-source-ao3") {
			t.Fatalf("verify %d: the literal field is the one that can collide run after run: %v\n%s", i+1, err, out)
		}
	}
}
