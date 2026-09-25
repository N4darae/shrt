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
)

type shortSessionBackend struct {
	mu     sync.Mutex
	uses   int
	logins int
	left   map[string]int
	short  bool
}

func (b *shortSessionBackend) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/Login") {
			b.logins++
			token := fmt.Sprintf("tok-%d", b.logins)
			b.left[token] = b.uses
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "access_token": token,
				"expires_at": fmt.Sprint(time.Now().Add(time.Hour).Unix())})
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		n, known := b.left[token]
		if !known || b.short && n <= 0 {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "unauthenticated", "message": "invalid or expired token"})
			return
		}
		b.left[token] = n - 1
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "widget"})
	}))
}

func shortSessionWorkspace(t *testing.T, b *shortSessionBackend, steps string) {
	t.Helper()
	b.left = map[string]int{}
	srv := b.server()
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

const lifetimeWrites = `    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect: [{path: error.code, equals: OK}, {path: id, equals: thing-1}]
    - id: create_again
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect: [{path: error.code, equals: OK}, {path: id, equals: thing-1}]
`

func TestATokenRefusedLongBeforeItsStatedExpiryInTwoRunsIsAFindingNotARestart(t *testing.T) {
	b := &shortSessionBackend{uses: 1, short: true}
	shortSessionWorkspace(t, b, lifetimeWrites)
	ctx := context.Background()
	var err error
	out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("one early refusal is no verdict: a restart since the login explains it too, want exit 3: %v\n%s", err, out)
	}
	if strings.Contains(out, "likely restarted") || strings.Contains(out, "FINDING") {
		t.Fatalf("nothing shows a restart and nothing repeats yet:\n%s", out)
	}
	if !strings.Contains(out, "WARNING: token refused 0s after issue although the login said it expires in 3600s") {
		t.Fatalf("the run must say the token died long before the expiry its login stated:\n%s", out)
	}
	out = captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if err == nil || errors.As(err, &coded) {
		t.Fatalf("a token this run's login issued refused early again, as in the previous run, is a finding, exit 1: %v\n%s", err, out)
	}
	if !strings.Contains(out, "FINDING: token refused 0s after issue although the login said it expires in 3600s") ||
		strings.Contains(out, "specific to that rpc") || strings.Contains(out, "likely restarted") {
		t.Fatalf("the finding is about the token's lifetime, not the rpc and not a restart:\n%s", out)
	}
}

func TestTwoTokensOfOneRunRefusedEarlyAreAFindingEvenWhenEveryReadWasResent(t *testing.T) {
	b := &shortSessionBackend{uses: 1, short: true}
	shortSessionWorkspace(t, b, `    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]
    - id: fetch_again
      call: ThingService/Fetch
      body: {id: thing-1}
      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]
    - id: fetch_third
      call: ThingService/Fetch
      body: {id: thing-1}
      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]
`)
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	var coded *exitError
	if err == nil || errors.As(err, &coded) {
		t.Fatalf("the re-login's own token was refused early too: a finding, exit 1, though every step passed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "PASSED") || !strings.Contains(out, "FINDING: token refused") ||
		!strings.Contains(out, "the fresh token the re-login issued was refused") {
		t.Fatalf("the run passed step by step and the finding names both refusals:\n%s", out)
	}
}

func TestVerifyReportsATokenLifetimeFindingAndNeverCallsItARestart(t *testing.T) {
	b := &shortSessionBackend{uses: 1}
	shortSessionWorkspace(t, b, lifetimeWrites)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	b.mu.Lock()
	b.short = true
	b.mu.Unlock()
	removeFile(t, ".shrt/tokens.json")
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out, "likely restarted") {
		t.Fatalf("one early refusal is could-not-verify, and not called a restart: %v\n%s", err, out)
	}
	if !strings.Contains(out, "token refused 0s after issue although the login said it expires in 3600s") {
		t.Fatalf("verify must name the token's age and stated lifetime:\n%s", out)
	}
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if err == nil || errors.As(err, &coded) || !strings.Contains(out, "FINDING: token refused 0s after issue") {
		t.Fatalf("refused early again in the next replay: a finding, exit 1: %v\n%s", err, out)
	}
}

func TestACachedTokenRefusedOnItsFirstUseSaysPossiblyARestart(t *testing.T) {
	b := &shortSessionBackend{uses: 100}
	shortSessionWorkspace(t, b, lifetimeWrites)
	ctx := context.Background()
	if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	b.mu.Lock()
	b.left = map[string]int{}
	b.mu.Unlock()
	var err error
	out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if !strings.Contains(out, "WARNING: token refused") || !strings.Contains(out, "the cached token, on its first use in this run") {
		t.Fatalf("the cached token was refused early on its first use: %v\n%s", err, out)
	}
	if strings.Contains(out, "nothing in this run shows a restart") {
		t.Fatalf("a restart since the token was cached leaves no trace in this run, so the run must not say none happened:\n%s", out)
	}
	if !strings.Contains(out, "possibly a restart since the token was cached") || strings.Contains(out, "FINDING") {
		t.Fatalf("the warning names a restart since the token was cached as a cause, and is no finding:\n%s", out)
	}
}
