package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

func chdirToRichWorkspace(t *testing.T) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/descriptor.binpb", string(catalogtest.RichDescriptor()))
}

func TestContractStatusGapsListsAnUncoveredStreamingRPCAsStreaming(t *testing.T) {
	chdirToRichWorkspace(t)
	var err error
	out := captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "WatchOrder") && !strings.HasPrefix(line, "streaming") {
			t.Fatalf("a streaming rpc is out of scope, listed as streaming, not as a gap:\n%s", out)
		}
	}
	if !strings.Contains(out, "streaming    shrt.test.rich.v1.") {
		t.Fatalf("the streaming rpcs must be listed:\n%s", out)
	}
}

func TestQualityGateNamesUncoveredRPCsWhenTheyRaiseTheScore(t *testing.T) {
	chdirToRichWorkspace(t)
	writeFile(t, ".shrt/quality-baseline", "0\n")
	var err error
	captureStdout(t, func() { err = contractQuality([]string{"-gate", "-baseline", ".shrt/quality-baseline"}) })
	if err == nil {
		t.Fatal("no overlay covers any rpc, so the score is above 0")
	}
	if !strings.Contains(err.Error(), "no overlay covers") || strings.Contains(err.Error(), "got vaguer") {
		t.Fatalf("the rise comes from uncovered rpcs, and the gate must say so: %v", err)
	}
}
