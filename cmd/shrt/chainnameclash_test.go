package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestTwoChainFilesWithOneNameAreRefusedByRunVerifyAndConfirm(t *testing.T) {
	approvedThingFlow(t)
	raw := string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml"))
	writeFile(t, ".shrt/chains/scratchy.yaml", strings.Replace(raw, "name: widget\n", "name: gadget\n", 1))
	before, _ := os.ReadDir(".shrt/runs/cli-thing-flow")
	ctx := context.Background()
	for _, tc := range []struct {
		cmd string
		run func() error
	}{
		{"run scratchy", func() error { return runRun(ctx, []string{"scratchy", "-quiet"}) }},
		{"run cli-thing-flow", func() error { return runRun(ctx, []string{"cli-thing-flow", "-quiet"}) }},
		{"verify scratchy", func() error { return runVerify(ctx, []string{"scratchy", "-quiet"}) }},
		{"confirm cli-thing-flow", func() error {
			return runConfirm(ctx, []string{"cli-thing-flow", "-supersede", "-note", "checked"})
		}},
	} {
		var err error
		out := captureStdout(t, func() { err = tc.run() })
		if err == nil || !strings.Contains(err.Error(), `2 chain files declare name "cli-thing-flow"`) ||
			!strings.Contains(err.Error(), ".shrt/chains/scratchy.yaml") || !strings.Contains(err.Error(), ".shrt/chains/cli-thing-flow.yaml") {
			t.Errorf("%s must refuse, naming both files: %v\n%s", tc.cmd, err, out)
		}
	}
	after, _ := os.ReadDir(".shrt/runs/cli-thing-flow")
	if len(after) != len(before) {
		t.Fatalf("nothing is recorded while two files claim one name: %d run(s) before, %d after", len(before), len(after))
	}
	var err error
	captureStdout(t, func() { err = runConfirm(ctx, []string{"cli-thing-flow", "-reject"}) })
	if err != nil && strings.Contains(err.Error(), "declare name") {
		t.Fatalf("-reject still works while the names clash, got %v", err)
	}
}

func TestSwappedChainNamesAreRefusedAndNotCalledOneChain(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	raw := string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml"))
	writeFile(t, ".shrt/chains/sw1.yaml", strings.Replace(raw, "name: cli-thing-flow\n", "name: sw2\n", 1))
	writeFile(t, ".shrt/chains/sw2.yaml", strings.Replace(raw, "name: cli-thing-flow\n", "name: sw1\n", 1))
	ctx := context.Background()
	for _, ref := range []string{"sw1", "sw2"} {
		var err error
		out := captureStdout(t, func() { err = runRun(ctx, []string{ref, "-quiet"}) })
		if err == nil || !strings.Contains(err.Error(), `"`+ref+`" names two different chains`) {
			t.Errorf("run %s is ambiguous (a file stem and another file's name) and must be refused: %v\n%s", ref, err, out)
		}
		out = captureStdout(t, func() { err = runVerify(ctx, []string{ref, "-quiet"}) })
		if err == nil || !strings.Contains(err.Error(), "names two different chains") {
			t.Errorf("verify %s must be refused the same way: %v\n%s", ref, err, out)
		}
	}
	var lerr error
	out := captureStdout(t, func() { lerr = runChain(ctx, []string{"lint", "sw1"}) })
	if strings.Contains(out, "verify the same chain") || !strings.Contains(out, "is another chain (name: sw1)") {
		t.Errorf("lint must not call two different chains one chain: %v\n%s", lerr, out)
	}
}

func TestAProposalNamesTheChainFileItsRunRan(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	var err error
	out := captureStdout(t, func() {
		if err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			return
		}
		err = runConfirm(ctx, []string{"cli-thing-flow", "-note", "checked the name"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "chain file `cli-thing-flow.yaml`") {
		t.Fatalf("the proposal names the chain file the run ran:\n%s", out)
	}
}

func TestPlainLintCountsTheWarningsStrictFails(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/bare.yaml", "apiVersion: shrt/v1\nname: bare\nsteps:\n  - id: fetch\n    call: shrt.test.v1.ThingService/Fetch\n    body: {id: x}\n")
	var err error
	out := captureStdout(t, func() { err = runChain(context.Background(), []string{"lint", "bare"}) })
	if err != nil || !strings.Contains(out, "exit 0, but 1 warning(s) above are errors under 'shrt chain lint -strict', which .shrt/ci-gate.sh runs\n") {
		t.Errorf("got %v:\n%s", err, out)
	}
}
