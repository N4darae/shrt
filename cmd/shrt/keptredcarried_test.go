package main

import (
	"strings"
	"testing"
)

func TestSliceKeptRedOfAChainWithoutPinsDoesNotClaimToCarryTheSourcesPins(t *testing.T) {
	oneDefectWorkspace(t)
	out, err := oneDefectSlice(t, "-step", "fetch", "-kept-red")
	if err != nil {
		t.Fatalf("slice -kept-red: %v\n%s", err, out)
	}
	if strings.Contains(out, "carries all") || strings.Contains(out, "pin(s) of cli-one-defect") {
		t.Fatalf("cli-one-defect has no kept_red, so the slice carries none of its pins:\n%s", out)
	}
	if !strings.Contains(out, "add -write to pin fetch at name") {
		t.Fatalf("the new pin is still reported:\n%s", out)
	}
}
