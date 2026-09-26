package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const uniqueNameChain = `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget ${vars.tag}
          kind: KIND_A
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
`

func newUniqueNameBackend() *httptest.Server {
	seen := map[string]bool{}
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			name, _ := body["name"].(string)
			if seen[name] {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "ALREADY_EXISTS", "message": "name " + name + " already exists"}})
				return
			}
			seen[name] = true
			next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": name})
		case "/shrt.test.v1.ThingService/Fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestVerifyNamesAReusedFixtureInsteadOfARegression(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-unique", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-unique", "-note", "unique names"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-unique", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	var err error
	captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=same1"}) })
	if err != nil {
		t.Fatalf("the first verify with a fresh tag is clean: %v", err)
	}
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=same1"}) })
	if err == nil {
		t.Fatal("a refused create is not a clean verify")
	}
	if strings.Contains(err.Error(), "regression") {
		t.Fatalf("the evidence is a reused fixture, not a regression: %v\n%s", err, out)
	}
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("a reused fixture is could-not-verify, exit 3, got %v", err)
	}
	if !strings.Contains(err.Error(), "fixture reused") || !strings.Contains(err.Error(), "tag=same1") || !strings.Contains(err.Error(), "-var tag=") {
		t.Fatalf("the verdict must name the reused var and suggest a fresh -var: %v", err)
	}
}
