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

func refusingUniqueBackend(t *testing.T, chainText string) (context.Context, *atomic.Bool) {
	t.Helper()
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
	writeFile(t, ".shrt/chains/cli-unique.yaml", chainText)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	return ctx, &refuseAll
}

func TestTwoFreshVarValuesInARowStayAFixtureCollision(t *testing.T) {
	ctx, refuseAll := refusingUniqueBackend(t, uniqueNameChain)
	refuseAll.Store(true)
	var err error
	var coded *exitError
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=fresh1"}) })
	if !errors.As(err, &coded) || coded.code != 3 || !strings.Contains(err.Error(), "fixture collision") {
		t.Fatalf("a first conflict on a fresh value is a fixture collision, exit 3: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=fresh2"}) })
	if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out, "FINDING") {
		t.Fatalf("another client may derive the same var values, so two in a row stay a fixture collision, exit 3: %v\n%s", err, out)
	}
	for _, want := range []string{"tag=fresh1", "tag=fresh2", "points at the backend unless another client uses the same values"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q in %v", want, err)
		}
	}
}

const uniqueUUIDChain = `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget ${vars.tag} ${uuid}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

func TestAConflictOnTwoUUIDBuiltValuesInARowIsAFinding(t *testing.T) {
	ctx, refuseAll := refusingUniqueBackend(t, uniqueUUIDChain)
	refuseAll.Store(true)
	var err error
	var coded *exitError
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=fresh1"}) })
	if !errors.As(err, &coded) || coded.code != 3 || !strings.Contains(err.Error(), "fixture collision") {
		t.Fatalf("a first conflict is a fixture collision, exit 3: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=fresh2"}) })
	if err == nil || errors.As(err, &coded) || !strings.Contains(out, "FINDING: ") {
		t.Fatalf("a value built from ${uuid} is unique to its run, so a repeat is a finding, exit 1: %v\n%s", err, out)
	}
}

func firstNonEmptyString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
