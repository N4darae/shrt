package main

import (
	"strings"
	"testing"
)

func TestChainWhichEndsOnItsCountsAndExplainsUnderV(t *testing.T) {
	fixWorkspace(t, &fixThing{fetchCode: "PERMISSION_DENIED"}, fixRefusalChain)
	if _, err := fixCmd(t, "run", "cli-refusal-flow", "-quiet"); err != nil {
		t.Fatal(err)
	}
	out := whichOut(t, "-rpc", "ThingService/Fetch")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "with a local run record that reached a matching step") || strings.Contains(out, "is what the chain claims") {
		t.Errorf("the footer is the one counts line:\n%s", out)
	}
	if verbose := whichOut(t, "-rpc", "ThingService/Fetch", "-v"); !strings.Contains(verbose, "asserted is what the chain claims") || !strings.Contains(verbose, "The plain closure keeps") {
		t.Errorf("-v explains the marks and the reproduce: line:\n%s", verbose)
	}
}
