package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUndeclaredFieldsArePrintedOnceGroupedByRPC(t *testing.T) {
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		next++
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": body["name"], "tier": "gold"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	for _, quiet := range []bool{true, false} {
		args := []string{"cli-unique", "-var", "tag=g" + itoa(next)}
		if quiet {
			args = append(args, "-quiet")
		}
		var err error
		out := captureStdout(t, func() { err = runRun(ctx, args) })
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		want := "the backend sends fields the proto does not declare: update the proto/descriptor if you want them compared"
		if n := strings.Count(out, want); n != 1 {
			t.Fatalf("quiet=%v: want the undeclared-field advice exactly once, got %d:\n%s", quiet, n, out)
		}
		if !strings.Contains(out, "ThingService/Create -> tier") || !strings.Contains(out, "ThingService/Fetch -> tier") {
			t.Fatalf("quiet=%v: the grouped line must name each rpc and its fields:\n%s", quiet, out)
		}
		if strings.Contains(out, "rebuild the descriptor") {
			t.Fatalf("quiet=%v: the backend is ahead of the proto, so rebuilding the descriptor does not help:\n%s", quiet, out)
		}
	}
}
