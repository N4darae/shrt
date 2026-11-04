package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestNullChainStepDoesNotBreakOtherCommands(t *testing.T) {
	t.Chdir(t.TempDir())
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/broken.yaml", "steps:\n  - call: ThingService/Fetch\n  -\n")
	var err error
	out := captureStdout(t, func() { err = chainList(nil) })
	if err != nil || !strings.Contains(out, "cli-thing-flow") || !strings.Contains(out, "broken.yaml") || !strings.Contains(out, "step 2") {
		t.Fatalf("list should show the valid chain and the broken file: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = chainLint(nil) })
	if err == nil || !strings.Contains(out, "broken.yaml") || !strings.Contains(out, "step 2") {
		t.Fatalf("lint should report the empty step: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow", "-dry-run"}) })
	if err != nil || !strings.Contains(out, "DRY-RUN OK") {
		t.Fatalf("the unrelated valid chain should still run: %v\n%s", err, out)
	}
	// With just the broken file left, gate must return a diagnostic, not send anything.
	if err := os.Remove(".shrt/chains/cli-thing-flow.yaml"); err != nil {
		t.Fatal(err)
	}
	saved := gateExec
	t.Cleanup(func() { gateExec = saved })
	gateExec = func(ctx context.Context, args []string) gateOutcome {
		err := runRun(ctx, args[1:])
		if err == nil || !strings.Contains(err.Error(), "step 2") {
			t.Fatalf("gate's run must reject the empty step: %v", err)
		}
		return gateOutcome{code: exitCodeOf(err), side: gateSidecar{Error: err.Error()}}
	}
	out = captureStdout(t, func() { err = runGate(context.Background(), []string{"-retry-wait", "0", "-hollow-baseline", ""}) })
	if err == nil || !strings.Contains(out+err.Error(), "broken") {
		t.Fatalf("gate should report the broken chain: %v\n%s", err, out)
	}
}

func TestVerifyRejectsANullStepInAnApprovedSafeSpot(t *testing.T) {
	t.Chdir(t.TempDir())
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	spot := &store.SafeSpot{Chain: "cli-thing-flow", RunID: "approved", ConfirmedBy: "reviewer",
		Steps: []*runner.StepRecord{{ID: "fetch", Status: runner.StatusPassed}}}
	spot.Digest = spot.ComputeDigest()
	spot.Steps[0] = nil
	raw, err := json.Marshal(spot)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/safespots/cli-thing-flow.json", string(raw))
	captureStdout(t, func() { err = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	if err == nil || !strings.Contains(err.Error(), "changed after it was approved") {
		t.Fatalf("verify should reject the edited approval: %v", err)
	}
	// The normal run path also reads the spot before executing.
	captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	if err != nil {
		t.Fatalf("an invalid spot must not panic or prevent an ordinary run: %v", err)
	}
}
