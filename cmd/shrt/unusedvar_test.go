package main

import (
	"strings"
	"testing"
)

func TestUnusedVarErrorSaysSoWhenTheChainReadsNoVars(t *testing.T) {
	msg := unusedVarError([]string{"tag"}, "probe", nil).Error()
	if strings.Contains(msg, "vars this chain reads: \n") || strings.HasSuffix(msg, "vars this chain reads: ") {
		t.Fatalf("an empty list reads as a truncated message: %q", msg)
	}
	if !strings.Contains(msg, "reads no vars") || !strings.Contains(msg, "-var tag") {
		t.Fatalf("want the error kept and the empty list worded, got %q", msg)
	}
}

func TestUnusedVarErrorListsTheVarsTheChainReads(t *testing.T) {
	msg := unusedVarError([]string{"tagg"}, "probe", []string{"run_tag", "sku"}).Error()
	if !strings.Contains(msg, "vars this chain reads: run_tag, sku") {
		t.Fatalf("got %q", msg)
	}
}
