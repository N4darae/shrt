package main

import (
	"strings"
	"testing"
)

func TestChainWhichHeadsEachChainWithItsReproduceLineAndExplainsUnderV(t *testing.T) {
	fixWorkspace(t, &fixThing{fetchCode: "PERMISSION_DENIED"}, fixRefusalChain)
	if _, err := fixCmd(t, "run", "cli-refusal-flow", "-quiet"); err != nil {
		t.Fatal(err)
	}
	out := whichOut(t, "-rpc", "ThingService/Fetch")
	if !strings.Contains(out, "\nshrt chain slice cli-refusal-flow -step outsider_reads\n  outsider_reads") || strings.Contains(out, "A row is") {
		t.Errorf("each chain heads with its reproduce command, its rows under it:\n%s", out)
	}
	if verbose := whichOut(t, "-rpc", "ThingService/Fetch", "-v"); !strings.Contains(verbose, "A row is a step") || !strings.Contains(verbose, "-keep writes keeps every earlier write") {
		t.Errorf("-v explains the chain headers and the rows:\n%s", verbose)
	}
}
