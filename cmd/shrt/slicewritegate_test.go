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
		"now part of every sweep",
		"mv .shrt/chains/cli-fresh-flow-slice-fetch.yaml .shrt/scratch/",
		"shrt run .shrt/scratch/cli-fresh-flow-slice-fetch.yaml",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("slice -write must say the file joins every sweep and gate, and how to keep it out (%q):\n%s", want, out)
		}
	}
}
