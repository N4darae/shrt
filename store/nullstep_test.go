package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestNullStepNeverMatchesASafeSpotDigest(t *testing.T) {
	for _, first := range []bool{true, false} {
		spot := approvedSpot()
		if first {
			spot.Steps[0] = nil
		} else {
			spot.Steps = append(spot.Steps, nil)
		}
		if spot.DigestMatches() || spot.DigestKind() != "" {
			t.Fatal("a null step must not retain approval")
		}
		spot.Digest = spot.ComputeDigest()
		if spot.DigestMatches() {
			t.Fatal("even a matching hash must not approve an empty step")
		}
	}
}

func TestNullStepInRunIsRejectedOnLoad(t *testing.T) {
	root := t.TempDir()
	s := New(filepath.Join(root, "runs"), filepath.Join(root, "spots"))
	path := s.runPath("c", "r")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// An old unsealed record must not bypass structural validation.
	if err := os.WriteFile(path, []byte(`{"chain":"c","run_id":"r","steps":[{"id":"a"},null]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.LoadRun("c", "r")
	if err == nil || !strings.Contains(err.Error(), "c/r") || !strings.Contains(err.Error(), "step 2") {
		t.Fatalf("want a run and step-specific error, got %v", err)
	}
	if _, err := s.SaveRun(&runner.Record{Chain: "c", RunID: "new", Steps: []*runner.StepRecord{nil}}); err == nil {
		t.Fatal("a malformed in-memory run must not be saved and sealed")
	}
}

func TestNullStepInSafeSpotIsRejectedOnLoad(t *testing.T) {
	root := t.TempDir()
	s := New(filepath.Join(root, "runs"), filepath.Join(root, "spots"))
	spot := approvedSpot()
	spot.Steps = append(spot.Steps, nil)
	path := s.SafeSpotPath(spot.Chain)
	if err := writeJSON(path, spot); err != nil {
		t.Fatal(err)
	}
	_, err := s.LoadSafeSpot(spot.Chain)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "step 2") {
		t.Fatalf("want the file and empty-step index, got %v", err)
	}
}
