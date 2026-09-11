package chain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestLoadFileRejectsMisspelledStepKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "typo.yaml")
	body := "apiVersion: shrt/v1\nname: typo\nsteps:\n  - id: a\n    call: X/Y\n    allow_failure: true\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := chain.LoadFile(p)
	if err == nil {
		t.Fatal("allow_failure is not a step key; a chain using it must not load, or a step that looks permitted to fail is silently required to pass")
	}
	if !strings.Contains(err.Error(), "allow_failure") {
		t.Fatalf("error must name the offending key, got: %v", err)
	}
}

func TestLoadFileRejectsMisspelledExpectKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "typo.yaml")
	body := "apiVersion: shrt/v1\nname: typo\nsteps:\n  - id: a\n    call: X/Y\n    expect:\n      - path: error.code\n        one_of: [OK, not_found]\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := chain.LoadFile(p); err == nil {
		t.Fatal("one_of is not an expectation key; accepting it turns an assertion into a no-op that still reads as checked")
	}
}

func TestEveryCommittedChainLoadsUnderStrictDecode(t *testing.T) {
	files, err := filepath.Glob("../../.shrt/chains/*.yaml")
	if err != nil || len(files) == 0 {
		t.Skip("no committed chains to check")
	}
	for _, p := range files {
		if _, err := chain.LoadFile(p); err != nil {
			t.Errorf("%s: %v", filepath.Base(p), err)
		}
	}
}
