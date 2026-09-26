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

const rewiredChain = `apiVersion: shrt/v1
name: cli-rewired
steps:
    - id: create_a
      call: ThingService/Create
      body:
          name: widget a
      expect:
          - path: error.code
            equals: OK
    - id: create_b
      call: ThingService/Create
      body:
          name: widget b
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create_a.id}
      expect:
          - path: error.code
            equals: OK
`

func newNamedThingBackend() *httptest.Server {
	next := 0
	names := map[string]any{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			id := "thing-" + itoa(next)
			names[id] = body["name"]
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id})
		case "/shrt.test.v1.ThingService/Fetch":
			id, _ := body["id"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id, "name": names[id]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestVerifyCallsARewiredReferenceAChainChange(t *testing.T) {
	srv := newNamedThingBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-rewired.yaml", rewiredChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-rewired", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-rewired", "-note", "fetch reads a"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-rewired", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	writeFile(t, ".shrt/chains/cli-rewired.yaml", strings.Replace(rewiredChain, "${create_a.id}", "${create_b.id}", 1))
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-rewired", "-quiet"}) })
	if err == nil {
		t.Fatalf("the rewired chain drifts, verify must not pass:\n%s", out)
	}
	if !strings.Contains(out, "chain differs from the confirmed run at fetch body.id (${create_a.id} -> ${create_b.id})") {
		t.Errorf("verify must name the rewired reference as a chain change:\n%s", out)
	}
	if !strings.HasPrefix(err.Error(), "drift after a chain change") {
		t.Errorf("a rewired reference is a chain edit, not a regression: %v\n%s", err, out)
	}
	raw, rerr := os.ReadFile(".shrt/safespots/cli-rewired.json")
	if rerr != nil || !strings.Contains(string(raw), `"body_refs"`) {
		t.Errorf("the safe spot keeps the body references it was built from: %v", rerr)
	}
}
