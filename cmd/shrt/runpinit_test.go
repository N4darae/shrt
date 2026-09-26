package main

import (
	"context"
	"strings"
	"testing"
)

func TestRunOfAFailingChainEndsWithThePinCommand(t *testing.T) {
	twoDefectWorkspace(t, "name", "gadget")
	var err error
	out := captureStdout(t, func() {
		err = runRun(context.Background(), []string{"cli-two-defects", "-keep-going", "-var", "tag=T30"})
	})
	if err == nil {
		t.Fatalf("the chain fails")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if last := lines[len(lines)-1]; last != "pin it: shrt chain pin cli-two-defects (re-runs with -keep-going when needed)" {
		t.Fatalf("the last line is the pin command, got %q\n%s", last, out)
	}
	if _, err := twoDefectSlice(t, "-step", "fetch", "-kept-red=fetch,fetch_again", "-write", ".shrt/chains/cli-two-defects.yaml"); err != nil {
		t.Fatalf("slice: %v", err)
	}
	out = captureStdout(t, func() {
		err = runRun(context.Background(), []string{"cli-two-defects", "-keep-going", "-var", "tag=T31"})
	})
	if err != nil || strings.Contains(out, "pin it:") {
		t.Fatalf("a kept-red chain gets no pin command: %v\n%s", err, out)
	}
}
