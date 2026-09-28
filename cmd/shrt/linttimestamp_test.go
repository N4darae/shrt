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
	out := captureStdout(t, func() { err = chainLint([]string{"-v", "cli-thing-flow"}) })
	if err != nil {
		t.Fatalf("a hint is not an error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[fetch] timestamp created_at unasserted") || !strings.Contains(out, "within:") {
		t.Fatalf("fetch returns created_at, verify masks it, and nothing asserts it; lint must say so and name a range rule:\n%s", out)
	}

	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml")),
		"          - path: name\n            equals: widget\n",
		"          - path: name\n            equals: widget\n          - path: created_at\n            lte: ${nowunix}\n", 1))
	out = captureStdout(t, func() { err = chainLint([]string{"-v", "cli-thing-flow"}) })
	if strings.Contains(out, "[fetch] timestamp created_at unasserted") {
		t.Fatalf("created_at is asserted now, so there is nothing to hint:\n%s", out)
	}
}

func TestChainLintFoldsUnassertedTimestampsIntoOneLineUnlessVerbose(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	raw := string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml"))
	writeFile(t, ".shrt/chains/cli-thing-copy.yaml", strings.Replace(raw, "name: cli-thing-flow", "name: cli-thing-copy", 1))
	var err error
	out := captureStdout(t, func() { err = chainLint(nil) })
	if err != nil {
		t.Fatalf("warnings are not errors: %v\n%s", err, out)
	}
	if n := strings.Count(out, "verify masks timestamps"); n != 1 {
		t.Fatalf("the explanation prints once per lint, got %d:\n%s", n, out)
	}
	if n := strings.Count(out, "timestamp"); n != 2 || !strings.Contains(out, "created_at (cli-thing-copy, cli-thing-flow): expect within:") {
		t.Fatalf("one line names every chain, field and fix, got %d mentions:\n%s", n, out)
	}
	out = captureStdout(t, func() { err = chainLint([]string{"-v"}) })
	if n := strings.Count(out, "[fetch] timestamp created_at unasserted; expect within:"); n != 2 || strings.Count(out, "verify masks timestamps") != 1 {
		t.Fatalf("-v names the step under each chain, explained once:\n%s", out)
	}
}
