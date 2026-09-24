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

func TestChainHollowNamesRunsOfAPathGivenChainAsScratchNotOrphans(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "widget"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	raw, err := os.ReadFile(".shrt/chains/cli-thing-flow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/scratch/probe.yaml", strings.Replace(string(raw), "name: cli-thing-flow", "name: probe", 1))
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{".shrt/scratch/probe.yaml", "-quiet"}); err != nil {
			t.Fatalf("shrt run by path: %v", err)
		}
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	out := captureStdout(t, func() { _ = chainHollow(nil) })
	if strings.Contains(out, "orphan  probe") || !strings.Contains(out, "scratch probe") {
		t.Fatalf("probe was run by path and its file exists, so its runs are scratch runs, not orphans:\n%s", out)
	}
}
