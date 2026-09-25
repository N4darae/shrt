package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

const sessionReadStep = `    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]
`

func realGate(t *testing.T) {
	t.Helper()
	sleep := gateSleep
	inProcessGate(t)
	gateSleep = sleep
}

func cacheASessionToken(t *testing.T) {
	t.Helper()
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("first run: %v", err)
		}
	})
}

func TestTheGateHoldsAFreshTokenAndReportsSessionsThatEndEarly(t *testing.T) {
	b := &shortSessionBackend{uses: 1000, life: 300 * time.Millisecond}
	shortSessionWorkspace(t, b, sessionReadStep)
	realGate(t)
	cacheASessionToken(t)
	time.Sleep(400 * time.Millisecond)
	out, code := runGateOut(t)
	if code != 1 || !strings.Contains(out, "checking session lifetime: holding a fresh token 1s\n") ||
		!strings.Contains(out, "FINDING: sessions end early: a fresh token was refused after 1s although the login said 3600s") {
		t.Fatalf("a fresh token refused twice after being held is a finding, exit 1, got %d:\n%s", code, out)
	}
	if strings.Contains(out, "refused early once") || strings.Count(out, "checking session lifetime") != 1 {
		t.Fatalf("the check settles the note and says once that it waits:\n%s", out)
	}
}

func TestTheGateCallsAnEarlyRefusalARestartWhenAHeldFreshTokenIsAccepted(t *testing.T) {
	b := &shortSessionBackend{uses: 1000}
	shortSessionWorkspace(t, b, sessionReadStep)
	realGate(t)
	restart := func() {
		b.mu.Lock()
		b.left = map[string]int{}
		b.mu.Unlock()
	}
	cacheASessionToken(t)
	restart()
	out, code := runGateOut(t)
	if code != 0 || strings.Contains(out, "FINDING") ||
		!strings.Contains(out, "session check: the early refusal of auth profile default was a restart: a fresh token held 1s was accepted") {
		t.Fatalf("a backend that restarted once is no finding, got %d:\n%s", code, out)
	}
	restart()
	out, code = runGateOut(t, "-no-session-check")
	if code != 0 || strings.Contains(out, "checking session lifetime") || !strings.Contains(out, "note: a token of auth profile default was refused early once") {
		t.Fatalf("-no-session-check leaves the note, got %d:\n%s", code, out)
	}
}
