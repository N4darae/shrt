package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const otherUniqueChain = `apiVersion: shrt/v1
name: cli-other
vars:
    label: first
steps:
    - id: make
      call: ThingService/Create
      body:
          name: widget ${vars.label}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

func TestAValueAnotherChainCreatedIsAReusedFixture(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	writeFile(t, ".shrt/chains/cli-other.yaml", otherUniqueChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	for _, tag := range []string{"shared1", "shared2"} {
		captureStdout(t, func() {
			if err := runRun(ctx, []string{"cli-other", "-quiet", "-var", "label=" + tag}); err != nil {
				t.Fatalf("shrt run cli-other: %v", err)
			}
		})
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=" + tag}) })
		var coded *exitError
		if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out, "FINDING") {
			t.Fatalf("chain cli-other created that value, so this is not a finding, exit 3: %v\n%s", err, out)
		}
		if !strings.Contains(err.Error(), "fixture reused") || !strings.Contains(err.Error(), "cli-other") {
			t.Fatalf("the verdict names the other chain whose run used the value: %v", err)
		}
	}
}

func TestAValueARunSentWithAnUnknownOutcomeIsAReusedFixture(t *testing.T) {
	var drop atomic.Bool
	seen := map[string]bool{}
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		name, _ := body["name"].(string)
		if r.URL.Path == "/shrt.test.v1.ThingService/Create" {
			if seen[name] {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "ALREADY_EXISTS", "message": "name " + name + " already exists"}})
				return
			}
			seen[name] = true
			if drop.Load() {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					conn.Close()
				}
				return
			}
		}
		next++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": firstNonEmptyString(name, "widget")})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	drop.Store(true)
	for _, tag := range []string{"dropped1", "dropped2"} {
		captureStdout(t, func() { _ = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=" + tag}) })
	}
	drop.Store(false)
	for _, tag := range []string{"dropped1", "dropped2"} {
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=" + tag}) })
		var coded *exitError
		if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out, "FINDING") {
			t.Fatalf("a run of this chain sent %s and its outcome is unknown, so this is not a finding, exit 3: %v\n%s", tag, err, out)
		}
		if !strings.Contains(err.Error(), "fixture reused") || !strings.Contains(err.Error(), "whether the call took effect is unknown") ||
			strings.Contains(err.Error(), "something else") {
			t.Fatalf("the verdict names the run that sent the value with an unknown outcome: %v", err)
		}
	}
}
