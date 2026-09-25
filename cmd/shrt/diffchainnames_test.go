package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLIDiffOfTwoChainNamesSaysItComparesRunsOfOneChain(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/other-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml")),
		"name: cli-thing-flow", "name: other-flow", 1))
	derr := runDiff(context.Background(), []string{"cli-thing-flow", "other-flow"})
	if exitCodeOf(derr) != 2 {
		t.Fatalf("could not compare must exit 2, got %v", derr)
	}
	if derr == nil || !strings.Contains(derr.Error(), "cli-thing-flow and other-flow are chain names, not run ids") ||
		!strings.Contains(derr.Error(), "shrt diff cli-thing-flow <run-a> <run-b>") || strings.Contains(derr.Error(), "no run cli-thing-flow") {
		t.Fatalf("two chain names should be recognised as chains, got %v", derr)
	}
	derr = runDiff(context.Background(), []string{"cli-thing-flow", "latest"})
	if derr == nil || !strings.Contains(derr.Error(), "cli-thing-flow is a chain name: with two arguments both are run ids") {
		t.Fatalf("a chain and one run should say how to name the runs, got %v", derr)
	}
}
