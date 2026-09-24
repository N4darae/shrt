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

func TestVerifyQuietStillPrintsStepWarnings(t *testing.T) {
	var extra atomic.Bool
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		next++
		out := map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": body["name"]}
		if extra.Load() {
			out["tier"] = "gold"
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	extra.Store(true)
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=w1"}) })
	if err != nil {
		t.Fatalf("an added field is backward compatible, so verify is clean: %v\n%s", err, out)
	}
	if !strings.Contains(out, "warning [") || !strings.Contains(out, "tier") {
		t.Fatalf("verify -quiet must still print the step warnings in its summary, as run -quiet does:\n%s", out)
	}
}
