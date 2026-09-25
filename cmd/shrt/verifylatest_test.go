package main

import (
	"context"
	"strings"
	"testing"
)

func TestVerifyRunLatestNamesTheRunItDiffed(t *testing.T) {
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
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
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	spot, err := e.store.LoadSafeSpot("cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	var stderr string
	captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", "latest"}) })
	})
	if !strings.Contains(stderr, "-run latest is run "+spot.RunID) {
		t.Fatalf("verify -run latest must name the run it picked (%s):\n%s", spot.RunID, stderr)
	}
	if !strings.Contains(stderr, "IS the run this safe spot was made from") {
		t.Fatalf("latest resolving to the safe spot's own run is a self-comparison and must say so:\n%s", stderr)
	}

	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("second run: %v", err)
		}
	})
	latest, err := e.store.LatestRun("cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", "latest"}) })
	})
	if err != nil {
		t.Fatalf("verify -run latest: %v", err)
	}
	if !strings.Contains(stderr, "-run latest is run "+latest.RunID) {
		t.Fatalf("verify -run latest must name %s:\n%s", latest.RunID, stderr)
	}
	if strings.Contains(stderr, "IS the run this safe spot") {
		t.Fatalf("a later run is not a self-comparison:\n%s", stderr)
	}
	if !strings.Contains(helpOf(t, "verify"), "latest") {
		t.Fatal("verify -h must document -run latest")
	}
}
