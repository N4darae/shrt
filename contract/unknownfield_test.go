package contract_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestLoadOverlayRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.yaml")
	body := "apiVersion: shrt/contract/v1\ndomain: x\nrpcs:\n  a.b.C/D:\n    summary: s\n    failuers:\n      - code: 1001\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := contract.LoadOverlay(p); err == nil {
		t.Fatal("a misspelled failuers block must not load: its codes would silently leave the coverage denominator")
	}
}

func TestEveryCommittedOverlayLoadsUnderStrictDecode(t *testing.T) {
	files, err := filepath.Glob("../../.shrt/contracts/*.yaml")
	if err != nil || len(files) == 0 {
		t.Skip("no committed overlays to check")
	}
	for _, p := range files {
		if _, err := contract.LoadOverlay(p); err != nil {
			t.Errorf("%s: %v", filepath.Base(p), err)
		}
	}
}
