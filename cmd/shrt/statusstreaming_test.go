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

func TestContractStatusGapsListsOnlyUncallableStreamingRPCsAsStreaming(t *testing.T) {
	chdirToRichWorkspace(t)
	var err error
	out := captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "WatchOrder") && strings.HasPrefix(line, "streaming") {
			t.Fatalf("a server-streaming rpc is callable, not out of scope:\n%s", out)
		}
		if strings.Contains(line, "UploadOrders") && !strings.HasPrefix(line, "streaming") {
			t.Fatalf("a client-streaming rpc is out of scope, listed as streaming:\n%s", out)
		}
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

func TestContractStatusGapsSaysAServerStreamingRPCIsReadOnlyToItsFirstMessage(t *testing.T) {
	chdirToRichWorkspace(t)
	var err error
	out := captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) })
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "server-streaming; only its first message is read, so what it sends later (updates on change) is not checked") {
			n++
			if !strings.Contains(line, "WatchOrder: ") || strings.Contains(line, "UploadOrders") {
				t.Fatalf("the line names the server-streaming rpc: %q", line)
			}
		}
	}
	if n != 1 {
		t.Fatalf("want one line for the one callable server-streaming rpc, got %d:\n%s", n, out)
	}
}
