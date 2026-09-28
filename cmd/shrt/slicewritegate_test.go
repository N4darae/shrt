package main

import (
	"strings"
	"testing"
)

func TestSliceWriteSaysTheFileJoinsEverySweep(t *testing.T) {
	freshTagWorkspace(t)
	out, err := freshTagSlice(t, "-step", "fetch", "-write")
	if err != nil {
		t.Fatalf("slice -write: %v\n%s", err, out)
	}
	for _, want := range []string{
		"cli-fresh-flow-slice-fetch.yaml",
		"lint, hollow and the gate run",
		"mv .shrt/chains/cli-fresh-flow-slice-fetch.yaml .shrt/scratch/",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("slice -write must say the file joins every sweep and gate, and how to keep it out (%q):\n%s", want, out)
		}
	}
}

func TestSliceWithoutWrittenOverTheChainItselfSaysNothingAboutJoiningTheSweep(t *testing.T) {
	freshTagWorkspace(t)
	out, err := freshTagSlice(t, "-without", "by_tag", "-write", ".shrt/chains/cli-fresh-flow.yaml")
	if err != nil {
		t.Fatalf("slice -without -write over the chain: %v\n%s", err, out)
	}
	if !strings.Contains(out, "written: .shrt/chains/cli-fresh-flow.yaml") {
		t.Fatalf("the chain is rewritten in place:\n%s", out)
	}
	if strings.Contains(out, "lint, hollow and the gate run") {
		t.Fatalf("the chain was already part of every sweep; replacing it is not news:\n%s", out)
	}
}
