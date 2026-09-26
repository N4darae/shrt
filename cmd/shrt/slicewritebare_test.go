package main

import (
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestSliceWriteOfABareFileNameLandsBesideTheSourceChain(t *testing.T) {
	freshTagWorkspace(t)
	out, err := freshTagSlice(t, "-step", "fetch", "-write", "kept.yaml")
	if err != nil {
		t.Fatalf("slice -write kept.yaml: %v\n%s", err, out)
	}
	if _, err := os.Stat("kept.yaml"); err == nil {
		t.Fatalf("a bare file name is a chain file beside the source chain, not a file in the current directory:\n%s", out)
	}
	c, err := chain.LoadFile(".shrt/chains/kept.yaml")
	if err != nil || c.Name != "kept" {
		t.Fatalf("want .shrt/chains/kept.yaml named kept: %v %v\n%s", err, c, out)
	}
	if !strings.Contains(out, "so lint, hollow and the gate run it") {
		t.Fatalf("a slice written into paths.chains joins the gate, and says so:\n%s", out)
	}
}

func TestSliceWriteOfADotSlashPathStaysInTheCurrentDirectory(t *testing.T) {
	freshTagWorkspace(t)
	out, err := freshTagSlice(t, "-step", "fetch", "-write", "./here.yaml")
	if err != nil {
		t.Fatalf("slice -write ./here.yaml: %v\n%s", err, out)
	}
	if _, err := os.Stat("here.yaml"); err != nil {
		t.Fatalf("a value with a slash is a path, written exactly there: %v\n%s", err, out)
	}
	if _, err := os.Stat(".shrt/chains/here.yaml"); err == nil {
		t.Fatalf("a path must not be redirected into paths.chains:\n%s", out)
	}
}

func TestSliceWithoutWriteOfTheChainsOwnFileNameReplacesTheChain(t *testing.T) {
	oneDefectWorkspace(t)
	out, err := oneDefectSlice(t, "-without", "failed", "-write", "cli-one-defect.yaml")
	if err != nil {
		t.Fatalf("slice -without failed -write cli-one-defect.yaml: %v\n%s", err, out)
	}
	if _, err := os.Stat("cli-one-defect.yaml"); err == nil {
		t.Fatalf("-write <chain>.yaml replaces the chain, it writes no copy in the current directory:\n%s", out)
	}
	c, err := chain.LoadFile(".shrt/chains/cli-one-defect.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, s := range c.Steps {
		ids = append(ids, s.ID)
	}
	if c.Name != "cli-one-defect" || strings.Join(ids, ",") != "create,other" {
		t.Fatalf("the chain itself is replaced by the steps that still run: %s %v\n%s", c.Name, ids, out)
	}
}
