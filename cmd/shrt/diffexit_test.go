package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIDiffFollowsTheGlobalExitCodeForAFlagOrASetupItCannotLoad(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	var derr error
	captureStderr(t, func() { derr = runDiff(context.Background(), []string{"-nope"}) })
	if exitCodeOf(derr) != 1 {
		t.Fatalf("a flag diff cannot parse must exit 1 like every other command, got %d (%v)", exitCodeOf(derr), derr)
	}
	if err := os.Remove(filepath.Join(".shrt", "config.yaml")); err != nil {
		t.Fatal(err)
	}
	derr = runDiff(context.Background(), []string{"cli-thing-flow"})
	if exitCodeOf(derr) != 1 {
		t.Fatalf("a setup diff cannot load must exit 1 like every other command, got %d (%v)", exitCodeOf(derr), derr)
	}
	out := helpOf(t, "diff")
	if !strings.Contains(out, "a flag that cannot be parsed") || strings.Contains(out, "or bad usage") {
		t.Fatalf("diff -h must say a flag it cannot parse exits 1:\n%s", out)
	}
}
