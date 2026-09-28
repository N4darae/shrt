package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func writeVerifiedProbe(t *testing.T) string {
	t.Helper()
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	captureStdout(t, func() {
		if err := chainSlice(context.Background(), []string{"cli-thing-flow", "-step", "fetch", "-run", "latest", "-verify", "-write", "probe"}); err != nil {
			t.Fatalf("slice -verify -write: %v", err)
		}
	})
	path := ".shrt/chains/probe.yaml"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "VERIFIED by") {
		t.Fatalf("the verified slice must carry its verdict:\n%s", raw)
	}
	return path
}

func TestCLISliceWriteKeepsTheVerdictOfAnIdenticalVerifiedSlice(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	path := writeVerifiedProbe(t)
	var err error
	captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-thing-flow", "-step", "fetch", "-write", "probe"})
	})
	if err != nil {
		t.Fatalf("re-writing the same slice must succeed: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "VERIFIED by") || strings.Contains(string(raw), "HYPOTHESIS") {
		t.Fatalf("re-writing an identical slice must keep its VERIFIED verdict:\n%s", raw)
	}
}

func TestCLISliceWriteRefusesToReplaceAVerifiedSlice(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	path := writeVerifiedProbe(t)
	source := string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml"))
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(source, "equals: widget", "equals: gadget", 1))
	var err error
	captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-thing-flow", "-step", "fetch", "-write", "probe"})
	})
	if err == nil || !strings.Contains(err.Error(), "VERIFIED") {
		t.Fatalf("replacing a verified slice with a different one must be refused, naming the verdict: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "VERIFIED by") {
		t.Fatalf("a refused write must leave the verified slice alone:\n%s", raw)
	}
}
