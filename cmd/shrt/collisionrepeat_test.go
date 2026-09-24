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

func TestAUniquenessConflictOnTwoFreshValuesInARowIsAFinding(t *testing.T) {
	var refuseAll atomic.Bool
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		name, _ := body["name"].(string)
		if r.URL.Path == "/shrt.test.v1.ThingService/Create" && refuseAll.Load() {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "ALREADY_EXISTS", "message": "name " + name + " already exists"}})
			return
		}
		next++
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": firstNonEmptyString(name, "widget")})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	refuseAll.Store(true)
	var err error
	var coded *exitError
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=fresh1"}) })
	if !errors.As(err, &coded) || coded.code != 3 || !strings.Contains(err.Error(), "fixture collision") {
		t.Fatalf("a first conflict on a fresh value is a fixture collision, exit 3: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=fresh2"}) })
	if err == nil || errors.As(err, &coded) {
		t.Fatalf("the previous run collided at the same step with another fresh value, so this is a finding, exit 1: %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "tag=fresh1") || !strings.Contains(err.Error(), "tag=fresh2") {
		t.Fatalf("the finding must name both values: %v", err)
	}
	if !strings.Contains(out, "FINDING: ") {
		t.Fatalf("verify prints the finding line:\n%s", out)
	}
}

func firstNonEmptyString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
