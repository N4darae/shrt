package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRefusesAScratchFileNamedLikeAChainInPathsChains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "widget"})
	}))
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, filepath.Join("scratch", "fake.yaml"), `name: cli-thing-flow
steps:
    - id: only
      call: ThingService/Fetch
      body:
          id: thing-1
      expect:
          - path: error.code
            equals: OK
`)
	var err error
	captureStdout(t, func() {
		err = runRun(context.Background(), []string{filepath.Join("scratch", "fake.yaml"), "-quiet"})
	})
	if err == nil {
		t.Fatal("a scratch chain named like a chain in paths.chains must not run into that chain's runs")
	}
	for _, want := range []string{"cli-thing-flow", "rename"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(".shrt", "runs", "cli-thing-flow")); len(entries) > 0 {
		t.Fatalf("the refused run was stored as the chain's run: %d file(s)", len(entries))
	}
	captureStdout(t, func() {
		err = runRun(context.Background(), []string{filepath.Join(".shrt", "chains", "cli-thing-flow.yaml"), "-quiet"})
	})
	if err != nil {
		t.Fatalf("the chain's own file given by path is that chain: %v", err)
	}
}
