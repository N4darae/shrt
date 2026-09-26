package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type resettableUniqueBackend struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (b *resettableUniqueBackend) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen = map[string]bool{}
}

func (b *resettableUniqueBackend) server() *httptest.Server {
	b.reset()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		name, _ := body["name"].(string)
		if b.seen[name] {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "REJECTED", "message": "name " + name + " is already registered"}})
			return
		}
		b.seen[name] = true
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": name})
	}))
}

func shortLiteralChain(name string) string {
	return `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: '` + name + `'
          idempotency_key: key ${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`
}

func TestAOneCharacterLiteralThatCollidesIsAChainDefect(t *testing.T) {
	b := &resettableUniqueBackend{}
	srv := b.server()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", shortLiteralChain("@"))
	ctx := context.Background()
	captureStdout(t, func() { _ = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=one"}) })
	out := captureStdout(t, func() { _ = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=two"}) })
	if !strings.Contains(out, "collides with itself") || !strings.Contains(out, "name is the literal @") {
		t.Fatalf("the refusal quotes the one-character literal the chain sends every run, so the chain collides with itself:\n%s", out)
	}
}

func TestALiteralAcceptedAgainOnlyAfterABackendResetStillCollidesWithItself(t *testing.T) {
	b := &resettableUniqueBackend{}
	srv := b.server()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", shortLiteralChain("fixed-widget"))
	ctx := context.Background()
	captureStdout(t, func() {
		_ = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=a1"})
		_ = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=a2"})
	})
	b.reset()
	captureStdout(t, func() { _ = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=a3"}) })
	out := captureStdout(t, func() { _ = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=a4"}) })
	if !strings.Contains(out, "collides with itself") {
		t.Fatalf("two runs accepted the literal, but each was the first after an empty backend and the run after the first was refused: "+
			"the literal never was accepted twice in a row, so the chain collides with itself:\n%s", out)
	}
}
