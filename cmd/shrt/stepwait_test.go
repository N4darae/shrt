package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/store"
)

func timedSessionWorkspace(t *testing.T, life time.Duration, steps string) {
	t.Helper()
	var mu sync.Mutex
	issued := map[string]time.Time{}
	logins := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/Login") {
			logins++
			token := fmt.Sprintf("tok-%d", logins)
			issued[token] = time.Now()
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "access_token": token,
				"expires_at": fmt.Sprint(time.Now().Add(time.Hour).Unix())})
			return
		}
		at, known := issued[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !known || life > 0 && time.Since(at) > life {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "unauthenticated", "message": "invalid or expired token"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "widget"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	t.Setenv("LIFE_USER", "staff")
	t.Setenv("LIFE_PASSWORD", "secret")
	writeFile(t, ".shrt/config.yaml", `target:
    base_url: `+srv.URL+`
descriptor:
    file: .shrt/descriptor.binpb
auth:
    call: shrt.test.v1.AuthService/Login
    body:
        username: ${env.LIFE_USER}
        password: ${env.LIFE_PASSWORD}
    token_path: access_token
    expires_path: expires_at
paths:
    chains: .shrt/chains
    runs: .shrt/runs
    safespots: .shrt/safespots
`)
	writeFile(t, filepath.Join(".shrt", "chains", "cli-thing-flow.yaml"), "apiVersion: shrt/v1\nname: cli-thing-flow\nsteps:\n"+steps)
}

const heldFetches = `    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]
    - id: fetch_held
      call: ThingService/Fetch
      wait: 400ms
      body: {id: thing-1}
      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]
    - id: fetch_held_again
      call: ThingService/Fetch
      wait: 400ms
      body: {id: thing-1}
      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]
`

func TestAWaitInOneShortChainProvesABackendEndsSessionsEarly(t *testing.T) {
	timedSessionWorkspace(t, 200*time.Millisecond, heldFetches)
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow"}) })
	var coded *exitError
	if err == nil || errors.As(err, &coded) {
		t.Fatalf("two sessions ended long before their stated expiry in one run are a finding, exit 1: %v\n%s", err, out)
	}
	if !strings.Contains(out, "FINDING: token refused 0s after issue") || !strings.Contains(out, "the fresh token the re-login issued was refused") {
		t.Fatalf("the finding names both early refusals:\n%s", out)
	}
	if !strings.Contains(out, "sent after waiting") {
		t.Fatalf("the progress line says the step waited:\n%s", out)
	}
}

func TestAWaitAgainstABackendThatKeepsSessionsPassesAndIsNotLatency(t *testing.T) {
	timedSessionWorkspace(t, 0, heldFetches)
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	if err != nil || strings.Contains(out, "WARNING") || strings.Contains(out, "FINDING") {
		t.Fatalf("a backend that keeps its sessions passes the held chain: %v\n%s", err, out)
	}
	s := store.New(".shrt/runs", ".shrt/safespots")
	ids, _ := s.ListRuns("cli-thing-flow")
	rec, err := s.LoadRun("cli-thing-flow", ids[len(ids)-1])
	if err != nil {
		t.Fatal(err)
	}
	held, _ := rec.Step("fetch_held")
	if held.WaitedMS < 400 || held.LatencyMS >= 400 {
		t.Fatalf("the record keeps the wait apart from the call's latency: waited %dms, latency %dms", held.WaitedMS, held.LatencyMS)
	}
}

func TestADryRunDoesNotWait(t *testing.T) {
	timedSessionWorkspace(t, 0, strings.ReplaceAll(heldFetches, "400ms", "10m"))
	start := time.Now()
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-dry-run", "-quiet"}); err != nil {
			t.Fatalf("dry run: %v", err)
		}
	})
	if took := time.Since(start); took > 30*time.Second {
		t.Fatalf("a dry run sends nothing, so it waits for nothing; it took %s", took)
	}
}

func TestAWaitOutsideItsBoundsDoesNotLoad(t *testing.T) {
	for _, wait := range []string{"11m", "0s", "-5s", "soon", "25"} {
		c := &chain.Chain{Name: "w", Steps: []*chain.Step{{ID: "s", Call: "ThingService/Fetch", Wait: wait}}}
		if err := c.Normalize(); err == nil || !strings.Contains(err.Error(), "wait") {
			t.Fatalf("wait %q must be refused at load, naming the key: %v", wait, err)
		}
	}
	c := &chain.Chain{Name: "w", Steps: []*chain.Step{{ID: "s", Call: "ThingService/Fetch", Wait: "10m"}}}
	if err := c.Normalize(); err != nil {
		t.Fatalf("the maximum itself is allowed: %v", err)
	}
}
