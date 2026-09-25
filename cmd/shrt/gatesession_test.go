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
	if code != 1 || !strings.Contains(out, "checking session lifetime of auth profile default: holding a fresh token 1.") ||
		!strings.Contains(out, "FINDING: sessions of auth profile default end early: fresh tokens were refused at 1.") ||
		!strings.Contains(out, "s although the login said 3600s") {
		t.Fatalf("a fresh token refused after the long hold and another at half of it is a finding, exit 1, got %d:\n%s", code, out)
	}
	if strings.Contains(out, "refused early once") || strings.Count(out, "checking session lifetime") != 1 || !strings.Contains(out, "(-no-session-check skips this)\n") {
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
		!strings.Contains(out, "session check: the early refusal of auth profile default was a restart: a fresh token held 1") {
		t.Fatalf("a backend that restarted once is no finding, got %d:\n%s", code, out)
	}
	restart()
	out, code = runGateOut(t, "-no-session-check")
	if code != 0 || strings.Contains(out, "checking session lifetime") || !strings.Contains(out, "note: a token of auth profile default was refused early once") {
		t.Fatalf("-no-session-check leaves the note, got %d:\n%s", code, out)
	}
}

func TestTheGateNarrowsASessionLifetimeBetweenAnAcceptedAndARefusedAge(t *testing.T) {
	b := &shortSessionBackend{uses: 1000, life: 800 * time.Millisecond}
	shortSessionWorkspace(t, b, sessionReadStep)
	realGate(t)
	cacheASessionToken(t)
	b.mu.Lock()
	b.left = map[string]int{}
	b.mu.Unlock()
	start := time.Now()
	out, code := runGateOut(t)
	if code != 1 || !strings.Contains(out, "FINDING: sessions of auth profile default end early: fresh tokens were accepted at 0.") ||
		!strings.Contains(out, "s (twice) although the login said 3600s") {
		t.Fatalf("accepted at half the hold and refused at the hold twice is a finding naming both ages, got %d:\n%s", code, out)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the check adds at most two holds, took %s", took)
	}
}

func TestTheGateCallsARefusalARestartWhenBothLaterTokensAreAccepted(t *testing.T) {
	b := &shortSessionBackend{uses: 1000}
	shortSessionWorkspace(t, b, sessionReadStep)
	realGate(t)
	cacheASessionToken(t)
	restart := func() {
		b.mu.Lock()
		b.left = map[string]int{}
		b.mu.Unlock()
	}
	restart()
	slept := 0
	gateSleep = func(ctx context.Context, d time.Duration) {
		time.Sleep(d)
		if slept++; slept == 1 {
			restart()
		}
	}
	out, code := runGateOut(t)
	if code != 0 || strings.Contains(out, "FINDING") || !strings.Contains(out, "was refused once, then fresh ones held 0.") {
		t.Fatalf("one refusal at the long hold, from a restart during it, is no finding, got %d:\n%s", code, out)
	}
}

func TestTheGateEndsWithOneCoverageLineWhenNoContractPlansTheSuite(t *testing.T) {
	gateWorkspace(t, nil)
	out, code := runGateOut(t)
	if code != 0 || strings.Count(out, "coverage: ") != 1 ||
		!strings.Contains(out, "rpc(s) have no contract, so no planned probes (boundaries, other roles, missing tokens, read-backs); shrt contract init -all, then shrt contract plan -all -write\n") {
		t.Fatalf("no overlay: one coverage line, and the exit code stays the gate's, got %d:\n%s", code, out)
	}
	captureStdout(t, func() {
		if err := commands["contract"].run(context.Background(), []string{"init", "-all"}); err != nil {
			t.Fatalf("contract init: %v", err)
		}
	})
	out, code = runGateOut(t)
	if code != 0 || strings.Count(out, "coverage: ") != 1 ||
		!strings.Contains(out, "rpc(s) with a contract have no chain calling them, so none of their planned probes run: shrt contract plan -all -write\n") {
		t.Fatalf("contracts for rpcs no chain calls: one coverage line, exit unchanged, got %d:\n%s", code, out)
	}
}

func TestTheSessionHoldIsCappedAtThirtySecondsAndItsHalfAtFifteen(t *testing.T) {
	hold := sessionHold(gateEarly{age: 79 * time.Second, stated: time.Hour})
	if hold != 30*time.Second || hold/2 != 15*time.Second {
		t.Fatalf("a routine restart must not cost a gate more than 30s and 15s of holding, got %s", hold)
	}
	if got := sessionHold(gateEarly{age: 2 * time.Second}); got != 3*time.Second {
		t.Fatalf("a young refusal is held a second past its age, got %s", got)
	}
}
