package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLIVerifyCleanPrintsItsVerdictFirstAndNoFooter(t *testing.T) {
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
	if !strings.HasPrefix(out, "cli-thing-flow: no drift vs safe spot ") || strings.Contains(out, "covers the") || strings.Contains(out, "ok ") {
		t.Fatalf("a clean verify leads with its verdict, with no per-step progress and no footer:\n%s", out)
	}
}
