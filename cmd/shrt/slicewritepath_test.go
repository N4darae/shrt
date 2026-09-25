package main

import (
	"os"
	"strings"
	"testing"
)

func TestSliceWriteToAPathWritesExactlyThereAndSaysNoSweepReadsIt(t *testing.T) {
	freshTagWorkspace(t)
	out, err := freshTagSlice(t, "-step", "fetch", "-write", ".shrt/scratch/x")
	if err != nil {
		t.Fatalf("slice -write .shrt/scratch/x: %v\n%s", err, out)
	}
	if _, err := os.Stat(".shrt/scratch/x.yaml"); err != nil {
		t.Fatalf("a -write value with a slash is a path: want .shrt/scratch/x.yaml, got %v\n%s", err, out)
	}
	if _, err := os.Stat(".shrt/chains/.shrt"); err == nil {
		t.Fatalf("a path must not be nested under paths.chains:\n%s", out)
	}
	for _, want := range []string{
		"/.shrt/scratch/x.yaml\n",
		"outside .shrt/chains",
		"shrt run .shrt/scratch/x.yaml",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	for _, bad := range []string{"part of every sweep", "mv "} {
		if strings.Contains(out, bad) {
			t.Errorf("a slice written outside paths.chains joins no sweep, so %q is false:\n%s", bad, out)
		}
	}
	data, err := os.ReadFile(".shrt/scratch/x.yaml")
	if err != nil || !strings.Contains(string(data), "name: x\n") {
		t.Fatalf("the chain in x.yaml must be named x:\n%s", data)
	}
	writeFile(t, ".shrt/scratch/other.yaml", "hand written\n")
	if out, err := freshTagSlice(t, "-step", "fetch", "-write", ".shrt/scratch/other.yaml"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("an existing file at the path must not be overwritten: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(".shrt/scratch/other.yaml"); string(data) != "hand written\n" {
		t.Fatalf("the existing file was overwritten: %q", data)
	}
}

func TestSliceWriteToAPathDirectlyInPathsChainsSaysItJoinsTheGate(t *testing.T) {
	freshTagWorkspace(t)
	out, err := freshTagSlice(t, "-step", "fetch", "-write", ".shrt/chains/kept.yaml")
	if err != nil {
		t.Fatalf("slice: %v\n%s", err, out)
	}
	if _, err := os.Stat(".shrt/chains/kept.yaml"); err != nil {
		t.Fatalf("want .shrt/chains/kept.yaml: %v", err)
	}
	for _, want := range []string{"so lint, hollow and the gate run it", "mv .shrt/chains/kept.yaml .shrt/scratch/"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}
