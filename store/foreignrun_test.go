package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRunRefusesARecordOfAnotherChainCopiedIntoItsDirectory(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-1")
	rec.Chain = "other-flow"
	path, err := s.SaveRun(rec)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(s.RunsDir, "thing-flow", "run-1.json")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(dest, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRun("thing-flow", "run-1"); err == nil || !strings.Contains(err.Error(), "other-flow") {
		t.Fatalf("a run recorded for other-flow is not a run of thing-flow, got %v", err)
	}
	if _, err := s.LoadRun("other-flow", "run-1"); err != nil {
		t.Fatalf("its own chain still loads it: %v", err)
	}
}
