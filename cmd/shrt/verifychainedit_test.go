package main

import (
	"context"
	"strings"
	"testing"
)

func TestVerifyNamesAnEditedChainAsTheCauseEvenWithAVar(t *testing.T) {
	regressed := false
	srv := newTotalBackend(&regressed)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-fixture-flow.yaml", fixtureFlowChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-fixture-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-fixture-flow", "-note", "total 300"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-fixture-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	writeFile(t, ".shrt/chains/cli-fixture-flow.yaml", strings.Replace(fixtureFlowChain, "equals: ${vars.total}", "equals: 301", 1))
	verify := func(args ...string) string {
		t.Helper()
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, append([]string{"cli-fixture-flow", "-quiet"}, args...)) })
		if err != nil {
			out += "\nERR: " + err.Error()
		}
		return out
	}
	out := verify("-var", "tag=edited")
	if !strings.Contains(out, "chain differs from the confirmed run at fetch expect") || !strings.Contains(out, "ERR: drift after a chain change") {
		t.Errorf("the edited expectation is the chain's change, and it explains the failing step:\n%s", out)
	}
	if strings.Contains(out, "the chain file is not what differs") || strings.Contains(out, "ERR: regression") {
		t.Errorf("the chain file did change; it is not a backend regression:\n%s", out)
	}
	out = verify("-var", "tag=edited2", "-var", "kind=KIND_B")
	if strings.Contains(out, "the chain file is not what differs") {
		t.Errorf("a var and the chain file both changed; do not say the chain file did not:\n%s", out)
	}
	if !strings.Contains(out, "kind=KIND_B") || !strings.Contains(out, "fetch expect") {
		t.Errorf("name both causes, the var and the chain edit:\n%s", out)
	}
}
