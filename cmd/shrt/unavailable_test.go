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

func TestVerifyAGatewayUnavailableIsCouldNotVerify(t *testing.T) {
	var down atomic.Bool
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/shrt.test.v1.ThingService/Fetch" && down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"unavailable","message":"upstream connect error"}`))
			return
		}
		next++
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": body["name"]})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	down.Store(true)
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=gw1"}) })
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(err.Error(), "regression") {
		t.Fatalf("an unavailable answer from a gateway is could-not-verify, exit 3, got %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=gw2"}) })
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("run: an unavailable answer is error, exit 3, got %v\n%s", err, out)
	}
}
