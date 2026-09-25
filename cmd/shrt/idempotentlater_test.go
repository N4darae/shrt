package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const idempotentAfterChangeChain = `apiVersion: shrt/v1
name: cli-idem-late
steps:
    - id: pre
      call: ThingService/Fetch
      body:
          id: pre
      expect:
          - path: error.code
            equals: OK
    - id: own
      call: ThingService/Create
      body:
          name: owner-${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: create
      call: ThingService/Create
      body:
          name: ${own.id}
          idempotency_key: fixed-key-late-1
      expect:
          - path: error.code
            equals: OK
`

func TestALiteralKeyReplayAfterAnEarlierRegressionIsNamedInANote(t *testing.T) {
	var regressed atomic.Bool
	var mu sync.Mutex
	next := 0
	byKey := map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/shrt.test.v1.ThingService/Fetch" {
			name := "before"
			if regressed.Load() {
				name = "after"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "pre", "name": name})
			return
		}
		key, _ := body["idempotency_key"].(string)
		if prior, ok := byKey[key]; ok && key != "" {
			_ = json.NewEncoder(w).Encode(prior)
			return
		}
		next++
		thing := map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": body["name"]}
		if key != "" {
			byKey[key] = thing
		}
		_ = json.NewEncoder(w).Encode(thing)
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-idem-late.yaml", idempotentAfterChangeChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-idem-late", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-idem-late", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-idem-late", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	regressed.Store(true)
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-idem-late", "-quiet"}) })
	var coded *exitError
	if err == nil || errors.As(err, &coded) || !strings.HasPrefix(err.Error(), "regression") {
		t.Fatalf("the change at pre comes before the replay, so it is still a regression, exit 1: %v\n%s", err, out)
	}
	if !strings.Contains(out, "note: step \"create\"") || !strings.Contains(out, "idempotent replay") || !strings.Contains(out, "fixed-key-late-1") {
		t.Fatalf("the output must still name the idempotent replay at create:\n%s", out)
	}
}
