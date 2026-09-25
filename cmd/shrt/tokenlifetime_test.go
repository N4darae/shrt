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

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

type shortSessionBackend struct {
	mu     sync.Mutex
	uses   int
	logins int
	left   map[string]int
	short  bool
	life   time.Duration
	born   map[string]time.Time
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
			if b.born == nil {
				b.born = map[string]time.Time{}
			}
			b.born[token] = time.Now()
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "access_token": token,
				"expires_at": fmt.Sprint(time.Now().Add(time.Hour).Unix())})
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		n, known := b.left[token]
		if !known || b.short && n <= 0 || b.life > 0 && time.Since(b.born[token]) > b.life {
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
	if !strings.Contains(out, "note: cached token refused") || strings.Contains(out, "WARNING") {
		t.Fatalf("the cached token was refused early on its first use: %v\n%s", err, out)
	}
	if strings.Contains(out, "nothing in this run shows a restart") {
		t.Fatalf("a restart since the token was cached leaves no trace in this run, so the run must not say none happened:\n%s", out)
	}
	if !strings.Contains(out, "possibly a restart since the token was cached") || strings.Contains(out, "FINDING") {
		t.Fatalf("the warning names a restart since the token was cached as a cause, and is no finding:\n%s", out)
	}
	if lines := nonEmptyLines(out); len(lines) != 2 || len(lines[1]) > 200 {
		t.Fatalf("a cached token refused on its first use is one short line under the verdict:\n%s", out)
	}
}

func TestReloginTokensRefusedEarlyThreeTimesInARowAreAFinding(t *testing.T) {
	b := &shortSessionBackend{uses: 100}
	shortSessionWorkspace(t, b, lifetimeWrites)
	ctx := context.Background()
	restart := func() {
		b.mu.Lock()
		b.left = map[string]int{}
		b.mu.Unlock()
	}
	var err error
	captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	restart()
	out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if err != nil || !strings.Contains(out, "note: cached token refused") || !strings.Contains(out, "since the token was cached") {
		t.Fatalf("the first early refusal of a cached token is the ambiguous note: %v\n%s", err, out)
	}
	restart()
	out = captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if err != nil || !strings.Contains(out, "note: cached token refused") || !strings.Contains(out, "possibly a second restart since the re-login") {
		t.Fatalf("the re-login's token refused early once is still a note, naming the earlier refusal: %v\n%s", err, out)
	}
	restart()
	out = captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	var coded *exitError
	if err == nil || errors.As(err, &coded) {
		t.Fatalf("a third early refusal in a row, each of the re-login's token, is a finding, exit 1: %v\n%s", err, out)
	}
	if !strings.Contains(out, "FINDING: token refused") || !strings.Contains(out, "one restart does not explain three refusals in a row") {
		t.Fatalf("the finding names the refusals the re-logins followed:\n%s", out)
	}
}

func TestAReloginTokenAcceptedFromTheCacheIsAnOrdinaryCachedTokenAgain(t *testing.T) {
	b := &shortSessionBackend{uses: 100}
	shortSessionWorkspace(t, b, lifetimeWrites)
	ctx := context.Background()
	restart := func() {
		b.mu.Lock()
		b.left = map[string]int{}
		b.mu.Unlock()
	}
	var err error
	captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	restart()
	captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if err != nil {
		t.Fatalf("the re-login's token is accepted from the cache: %v", err)
	}
	restart()
	out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if err != nil || strings.Contains(out, "FINDING") || !strings.Contains(out, "note: cached token refused") {
		t.Fatalf("a token accepted in a later run and then refused is explained by a restart since: %v\n%s", err, out)
	}
}

func TestAReloginTokenRefusedLongAfterTheEarlierRefusalStaysANote(t *testing.T) {
	issued := time.Date(2026, 9, 25, 19, 28, 6, 0, time.UTC)
	refusal := func(after time.Duration, relogins ...time.Time) *runner.Record {
		return &runner.Record{Steps: []*runner.StepRecord{{Index: 1, ID: "list", Status: runner.StatusPassed,
			TokenRefused: []transport.TokenRefusal{{Token: "5f8c09ca", IssuedAt: issued, ExpiresAt: issued.Add(time.Hour),
				RefusedAt: issued.Add(after), Cached: true, FirstUse: true, Relogins: relogins}}}}}
	}
	earlier := issued.Add(-22 * time.Second)
	if life := examineTokenLifetime(nil, refusal(38*time.Second, earlier, issued)); !life.finding() || life.label() != "FINDING: " ||
		!strings.Contains(life.line(), "after the refusal at 19:28:06Z") || !strings.Contains(life.line(), "at 19:27:44Z had issued") {
		t.Fatalf("the third refusal in a row, each within seconds of the last: a finding, got %q", life.line())
	}
	if life := examineTokenLifetime(nil, refusal(38*time.Second, issued)); life.finding() || life.label() != "note: " {
		t.Fatalf("one re-login token refused is still a note, got %q", life.line())
	}
	if life := examineTokenLifetime(nil, refusal(10*time.Minute, earlier.Add(-10*time.Minute), issued)); !life.finding() {
		t.Fatalf("refusals minutes apart, each well inside the hour the login stated, still chain: got %q", life.line())
	}
	if life := examineTokenLifetime(nil, refusal(38*time.Second, earlier.Add(-2*time.Hour), issued)); life.finding() {
		t.Fatalf("an earlier refusal longer ago than the stated lifetime does not chain: got %q", life.line())
	}
}
