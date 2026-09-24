package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLIVerifyWithAPendingProposalAsksForTheApproversEmail(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	captureStdout(t, func() {
		if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-note", "fetch returns the created name"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
	})
	err := runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"})
	if err == nil {
		t.Fatal("verify without a safe spot must fail")
	}
	if !strings.Contains(err.Error(), "shrt confirm cli-thing-flow -approve -by <their email>") || strings.Contains(err.Error(), "<name>") {
		t.Fatalf("verify must print the approval command confirm prints, -by <their email>: %v", err)
	}
}
