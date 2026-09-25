package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func newIdempotentBackend() *httptest.Server {
	var mu sync.Mutex
	next := 0
	byKey := map[string]map[string]any{}
	things := map[string]map[string]any{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			key, _ := body["idempotency_key"].(string)
			if prior, ok := byKey[key]; ok && key != "" {
				_ = json.NewEncoder(w).Encode(prior)
				return
			}
			next++
			thing := map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": body["name"]}
			byKey[key], things[thing["id"].(string)] = thing, thing
			_ = json.NewEncoder(w).Encode(thing)
		case "/shrt.test.v1.ThingService/Fetch":
			id, _ := body["id"].(string)
			_ = json.NewEncoder(w).Encode(things[id])
		}
	}))
}

const idempotentChain = `apiVersion: shrt/v1
name: cli-idem
vars:
    tag: a
    ik: key-one
steps:
    - id: create
      call: ThingService/Create
      body:
          name: w-${vars.tag}
          idempotency_key: KEY
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: w-${vars.tag}
`

func approveIdempotentChain(t *testing.T, key string) context.Context {
	t.Helper()
	srv := newIdempotentBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-idem.yaml", strings.Replace(idempotentChain, "KEY", key, 1))
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-idem", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-idem", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-idem", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	return ctx
}

func TestALiteralIdempotencyKeyReplayIsAChainDefectNotARegression(t *testing.T) {
	ctx := approveIdempotentChain(t, "fixed-key-ao-1")
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-idem", "-quiet", "-var", "tag=b"}) })
	var coded *exitError
	if err == nil || errors.As(err, &coded) || strings.HasPrefix(err.Error(), "regression") {
		t.Fatalf("the backend replayed the confirmed run's answer for the same key: a chain defect, exit 1, not a regression: %v\n%s", err, out)
	}
	for _, want := range []string{"idempotency_key", "fixed-key-ao-1", "${uuid}"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
	if !strings.Contains(out, "CHAIN DEFECT: ") {
		t.Errorf("the human output says it is a chain defect:\n%s", out)
	}
}

func TestAVarIdempotencyKeyReusedFromTheConfirmedRunIsAFixtureReuse(t *testing.T) {
	ctx := approveIdempotentChain(t, "${vars.ik}")
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-idem", "-quiet", "-var", "tag=b"}) })
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 || strings.HasPrefix(err.Error(), "regression") {
		t.Fatalf("the key equals the confirmed run's, so the backend replayed its answer: fixture reused, exit 3: %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "fixture reused") || !strings.Contains(err.Error(), "-var ik=") {
		t.Fatalf("say it is a reused key and how to send a fresh one: %v", err)
	}
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-idem", "-quiet", "-var", "tag=c", "-var", "ik=key-two"}) })
	if err != nil && strings.Contains(err.Error(), "fixture reused") {
		t.Fatalf("a fresh key is not a reuse: %v\n%s", err, out)
	}
}
