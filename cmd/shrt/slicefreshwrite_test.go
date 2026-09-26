package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestASliceVerifiedWithAFreshVarCarriesThatValueNotTheSourceDefault(t *testing.T) {
	freshTagWorkspace(t)
	if _, err := freshTagSlice(t, "-step", "fetch", "-run", "latest", "-verify", "-var", "tag=T2", "-write", "cli-fresh-slice"); err != nil {
		t.Fatalf("slice -verify -write with a fresh tag: %v", err)
	}
	path := filepath.Join(".shrt", "chains", "cli-fresh-slice.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.Vars["tag"]; v != "T2" {
		t.Fatalf("the slice was verified with -var tag=T2, so it carries that value, not the source default T1 its own runs created with, got tag: %v:\n%s", v, raw)
	}
	if !strings.Contains(c.Description, "=<fresh>") {
		t.Fatalf("the slice still says each run needs a fresh -var tag:\n%s", raw)
	}
}
