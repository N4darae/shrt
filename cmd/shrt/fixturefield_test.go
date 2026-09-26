package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const twoFixtureChain = `apiVersion: shrt/v1
name: cli-twofix
vars:
    tag: first
    stag: s0
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget ${vars.tag}
          kind: KIND_A
          meta:
              source: src-${vars.stag}
      expect:
          - path: error.code
            equals: OK
`

func newUniqueSourceBackend() *httptest.Server {
	seen := map[string]bool{}
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		meta, _ := body["meta"].(map[string]any)
		source, _ := meta["source"].(string)
		if seen[source] {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "SOURCE_TAKEN", "message": "already in use"}})
			return
		}
		seen[source] = true
		next++
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": body["name"]})
	}))
}

func TestFixtureReuseBlamesOnlyTheVarOfTheConflictingField(t *testing.T) {
	srv := newUniqueSourceBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-twofix.yaml", twoFixtureChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-twofix", "-quiet", "-var", "tag=A2", "-var", "stag=used1"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-twofix", "-note", "two fixtures"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-twofix", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	resp, err := http.Post(srv.URL+"/shrt.test.v1.ThingService/Create", "application/json", strings.NewReader(`{"name":"x","meta":{"source":"src-FRESH365"}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	out := captureStdout(t, func() {
		err = runVerify(ctx, []string{"cli-twofix", "-quiet", "-var", "tag=A2", "-var", "stag=FRESH365"})
	})
	if err == nil || strings.Contains(err.Error(), "fixture reused") || strings.Contains(err.Error(), "tag=A2") {
		t.Fatalf("the refusal is about the source, built from stag, whose value no run used: %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "fixture collision") || !strings.Contains(err.Error(), "stag=FRESH365") {
		t.Fatalf("the verdict must name the var of the conflicting field: %v", err)
	}
}
