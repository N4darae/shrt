package main

import (
	"strings"
	"testing"
)

func TestChainLintHintsAtATimestampNoExpectationReads(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	var err error
	out := captureStdout(t, func() { err = chainLint([]string{"cli-thing-flow"}) })
	if err != nil {
		t.Fatalf("a hint is not an error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "created_at (fetch)") || !strings.Contains(out, "within:") {
		t.Fatalf("fetch returns created_at, verify masks it, and nothing asserts it; lint must say so and name a range rule:\n%s", out)
	}

	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml")),
		"          - path: name\n            equals: widget\n",
		"          - path: name\n            equals: widget\n          - path: created_at\n            lte: ${nowunix}\n", 1))
	out = captureStdout(t, func() { err = chainLint([]string{"cli-thing-flow"}) })
	if strings.Contains(out, "created_at (fetch)") {
		t.Fatalf("created_at is asserted now, so there is nothing to hint:\n%s", out)
	}
}
