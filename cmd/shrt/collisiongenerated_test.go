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

const generatedNameChain = `apiVersion: shrt/v1
name: cli-genunique
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${uuid}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

func newRefusingBackend(refuse *atomic.Bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/shrt.test.v1.ThingService/Create" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		name, _ := body["name"].(string)
		if refuse.Load() {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "ALREADY_EXISTS", "message": "name " + name + " already exists"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": name})
	}))
}

func TestACollisionOnAPurelyGeneratedValueSaysAPlainRerunIsFresh(t *testing.T) {
	for _, command := range []string{"run", "verify"} {
		t.Run(command, func(t *testing.T) {
			var refuse atomic.Bool
			srv := newRefusingBackend(&refuse)
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			writeFile(t, ".shrt/chains/cli-genunique.yaml", generatedNameChain)
			ctx := context.Background()
			captureStdout(t, func() {
				if err := runRun(ctx, []string{"cli-genunique", "-quiet"}); err != nil {
					t.Fatalf("shrt run: %v", err)
				}
				if err := runConfirm(ctx, []string{"cli-genunique", "-note", "generated names"}); err != nil {
					t.Fatalf("propose: %v", err)
				}
				if err := runConfirm(ctx, []string{"cli-genunique", "-approve", "-by", "alice@example.test"}); err != nil {
					t.Fatalf("approve: %v", err)
				}
			})
			refuse.Store(true)
			var err error
			out := captureStdout(t, func() {
				if command == "run" {
					err = runRun(ctx, []string{"cli-genunique", "-quiet"})
				} else {
					err = runVerify(ctx, []string{"cli-genunique", "-quiet"})
				}
			})
			if err == nil {
				t.Fatal("a refused create does not pass")
			}
			text := out + "\n" + err.Error()
			if !strings.Contains(text, "fixture collision") {
				t.Fatalf("want a fixture collision:\n%s", text)
			}
			if strings.Contains(text, "fresh value: shrt "+command+" cli-genunique \n") || strings.HasSuffix(strings.TrimSpace(err.Error()), "cli-genunique") && strings.Contains(err.Error(), "with a fresh value") {
				t.Fatalf("the hint offers a command with nothing to pass:\n%s", text)
			}
			for _, line := range strings.Split(text, "\n") {
				if strings.HasSuffix(line, " ") {
					t.Errorf("line ends in a space: %q", line)
				}
			}
			if !strings.Contains(text, "a plain re-run generates a fresh value: shrt "+command+" cli-genunique") &&
				!strings.Contains(text, "A plain re-run generates a fresh value: shrt "+command+" cli-genunique") {
				t.Fatalf("the colliding value is built only from ${uuid}, so a plain re-run is the remedy:\n%s", text)
			}
		})
	}
}
