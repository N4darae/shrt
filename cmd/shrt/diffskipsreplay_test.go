package main

import (
	"context"
	"strings"
	"testing"
)

func TestDefaultDiffSkipsVerifyReplays(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "fetch returns the created name"}); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	name = "gadget"
	if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err == nil {
		t.Fatal("the gate run sees the changed name")
	}
	var verr error
	captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if verr == nil {
		t.Fatal("verify sees the changed name")
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := e.store.ListRuns("cli-thing-flow")
	if err != nil || len(ids) != 3 {
		t.Fatalf("want the approved run, the gate run and the replay, got %v %v", ids, err)
	}
	spot, err := e.store.LoadRun("cli-thing-flow", ids[0])
	if err != nil {
		t.Fatal(err)
	}
	replay, err := e.store.LoadRun("cli-thing-flow", ids[2])
	if err != nil {
		t.Fatal(err)
	}
	if replay.ReplayOf != spot.RunID {
		t.Fatalf("a verify replay records the safe spot's run it replays, got replay_of %q", replay.ReplayOf)
	}
	var derr error
	out := captureStdout(t, func() { derr = runDiff(ctx, []string{"cli-thing-flow"}) })
	if exitCodeOf(derr) != 1 {
		t.Fatalf("the gate run differs from the run before it: %v\n%s", derr, out)
	}
	if !strings.Contains(out, ids[0]) || !strings.Contains(out, ids[1]) || strings.Contains(out, "run B: "+ids[2]) {
		t.Fatalf("the default diff compares the two latest runs that are not verify replays (%s, %s):\n%s", ids[0], ids[1], out)
	}
}

func TestDefaultDiffAfterOnlyVerifyReplaysComparesTheNewestReplay(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "fetch returns the created name"}); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	name = "gadget"
	for range 2 {
		captureStdout(t, func() { _ = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := e.store.ListRuns("cli-thing-flow")
	if err != nil || len(ids) != 4 {
		t.Fatalf("want two runs and two replays, got %v %v", ids, err)
	}
	var derr error
	out := captureStdout(t, func() { derr = runDiff(ctx, []string{"cli-thing-flow"}) })
	if exitCodeOf(derr) != 1 || !strings.Contains(out, "run A "+ids[1]) || !strings.Contains(out, "run B "+ids[3]) {
		t.Fatalf("with no run beside the newest replays, the default diff compares the newest replay (%s) with the latest run (%s): %v\n%s", ids[3], ids[1], derr, out)
	}
}
