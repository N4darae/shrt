package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLIVerifyCleanSaysWhatNoDriftCovers(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-note", "fetch returns the created name"}); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	var err error
	out := captureStdout(t, func() {
		err = runVerify(context.Background(), []string{"cli-thing-flow"})
	})
	if err != nil {
		t.Fatalf("verify: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no drift") ||
		!strings.Contains(out, "covers the 2 step(s) of this chain only; a regression in a path no safe spot exercises is not seen\n") {
		t.Fatalf("a clean verify must say its no drift covers only this chain's steps:\n%s", out)
	}
}
