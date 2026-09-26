package main

import (
	"context"
	"strings"
	"testing"
)

func TestChainLintPrintsARepeatedWarningOnceAndOneShortLinePerLaterStep(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	writeFile(t, ".shrt/chains/cli-probe.yaml", `apiVersion: shrt/v1
name: cli-probe
steps:
    - id: fetch_1
      call: ThingService/Fetch
      body:
          id: thing-1
    - id: fetch_2
      call: ThingService/Fetch
      body:
          id: thing-2
    - id: fetch_3
      call: ThingService/Fetch
      body:
          id: thing-3
`)
	out := captureStdout(t, func() { _ = runChain(context.Background(), []string{"lint", "cli-probe"}) })
	if strings.Count(out, "asserts nothing at all") != 3 || strings.Count(out, "A step with no expect entry") != 1 {
		t.Fatalf("each step keeps its line and the explanation is printed once:\n%s", out)
	}
	for _, step := range []string{"fetch_2", "fetch_3"} {
		if !strings.Contains(out, "["+step+"] asserts nothing at all, as above\n") {
			t.Errorf("a later step with the same warning gets one short line:\n%s", out)
		}
	}
}
