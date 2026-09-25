package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestAChainNamedOtherwiseThanItsFileIsVerifiedAgainstItsOwnName(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	confirmAs := func(ref string) {
		t.Helper()
		captureStdout(t, func() {
			if err := runRun(ctx, []string{ref, "-quiet"}); err != nil {
				t.Fatalf("shrt run: %v", err)
			}
			if err := runConfirm(ctx, []string{ref, "-note", "baseline"}); err != nil {
				t.Fatalf("propose: %v", err)
			}
			if err := runConfirm(ctx, []string{ref, "-approve", "-by", "alice@example.test"}); err != nil {
				t.Fatalf("approve: %v", err)
			}
		})
	}
	confirmAs("cli-thing-flow")
	raw, err := os.ReadFile(".shrt/chains/cli-thing-flow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(string(raw), "name: cli-thing-flow\n", "name: thing-new\n", 1))

	for _, ref := range []string{"cli-thing-flow", "thing-new"} {
		var verr error
		out := captureStdout(t, func() { verr = runVerify(ctx, []string{ref, "-quiet"}) })
		if verr == nil || !strings.Contains(verr.Error(), "thing-new has no safe spot") {
			t.Errorf("verify %s: the chain is named thing-new, which has no safe spot; cli-thing-flow's must not be used: %v\n%s", ref, verr, out)
		}
	}

	confirmAs("cli-thing-flow")
	if _, err := os.Stat(".shrt/safespots/thing-new.json"); err != nil {
		t.Fatalf("confirming by the file name writes the safe spot of the chain's name: %v", err)
	}
	for _, ref := range []string{"cli-thing-flow", "thing-new"} {
		var verr error
		out := captureStdout(t, func() { verr = runVerify(ctx, []string{ref, "-quiet"}) })
		if verr != nil || !strings.Contains(out, "thing-new: no drift") {
			t.Errorf("verify %s compares against thing-new's safe spot: %v\n%s", ref, verr, out)
		}
	}

	var lerr error
	out := captureStdout(t, func() { lerr = runChain(ctx, []string{"lint", "cli-thing-flow"}) })
	if lerr != nil || !strings.Contains(out, "declares name: thing-new") || !strings.Contains(out, "rename the file to thing-new.yaml") {
		t.Errorf("lint warns about the mismatch and passes: %v\n%s", lerr, out)
	}
	out = captureStdout(t, func() { lerr = runChain(ctx, []string{"ls"}) })
	if lerr != nil || !strings.Contains(out, "* thing-new") || !strings.Contains(out, "file cli-thing-flow.yaml") {
		t.Errorf("chain ls lists thing-new with its safe spot and notes its file: %v\n%s", lerr, out)
	}
}
