package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func everyNth(from, n int) []int {
	var out []int
	for i := from; i <= 40; i += n {
		out = append(out, i)
	}
	return out
}

func inProcessGate(t *testing.T) *int {
	t.Helper()
	saved, savedSleep := gateExec, gateSleep
	t.Cleanup(func() { gateExec, gateSleep = saved, savedSleep })
	retries := 0
	gateSleep = func(context.Context, time.Duration) { retries++ }
	gateExec = func(ctx context.Context, args []string) gateOutcome {
		side := t.TempDir() + "/side.json"
		t.Setenv(gateReportEnv, side)
		var err error
		captureStdout(t, func() { err = commands[args[0]].run(ctx, args[1:]) })
		o := gateOutcome{code: exitCodeOf(err)}
		if raw, rerr := os.ReadFile(side); rerr == nil {
			_ = json.Unmarshal(raw, &o.side)
		}
		return o
	}
	return &retries
}

func TestAnUnavailableTheServiceAnswersBetweenAnsweredCallsIsAFinding(t *testing.T) {
	f, ctx := flakyWorkspace(t)
	f.set("unavailable", 503, everyNth(2, 4)...)
	var err error
	out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-flaky", "-quiet"}) })
	wantExit1(t, "run", err, out)
	if !strings.Contains(out, "FINDING: intermittent failure at ThingService/Fetch") {
		t.Fatalf("an unavailable with the service's own body, between calls of the same rpc it answered, is the rpc failing: %v\n%s", err, out)
	}
	f.set("unavailable", 503, everyNth(2, 4)...)
	out, err = verifyOnce(t, ctx)
	wantExit1(t, "verify", err, out)
	if !strings.Contains(err.Error(), "repeated failure at ThingService/Fetch") {
		t.Fatalf("the same unavailable at the same steps as the previous run is a repeated failure: %v\n%s", err, out)
	}
	f.set("unavailable", 503, everyNth(2, 4)...)
	retries := inProcessGate(t)
	var gateErr error
	out = captureStdout(t, func() { gateErr = runGate(ctx, []string{"cli-flaky"}) })
	if exitCodeOf(gateErr) != 1 || !strings.Contains(out, "FINDING    cli-flaky  repeated: ThingService/Fetch failed") || *retries != 0 {
		t.Fatalf("the gate fails the chain on the finding, without a retry: %v, %d retries\n%s", gateErr, *retries, out)
	}
	if !strings.Contains(out, "FINDING: repeated failure at ThingService/Fetch (failed ") || !strings.Contains(out, ") in 1 chain(s): the backend fails this rpc at the same calls every run, not by chance: a defect in the backend, and a re-run fails the same way\n") {
		t.Fatalf("the gate states the finding once:\n%s", out)
	}
}

func TestAnUnavailableTheServiceNeverAnswersAfterStaysNoVerdict(t *testing.T) {
	f, ctx := flakyWorkspace(t)
	f.set("unavailable", 503, everyNth(2, 1)...)
	var err error
	out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-flaky", "-quiet"}) })
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("run: unavailable at every call after the first, as in a restart, is no verdict, exit 3: %v\n%s", err, out)
	}
	f.set("unavailable", 503, everyNth(2, 1)...)
	out, err = verifyOnce(t, ctx)
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("verify: unavailable at every call after the first is could-not-verify, exit 3: %v\n%s", err, out)
	}
}
