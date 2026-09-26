package store_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestArchivedSafeSpotIsNamedByTheRunItHeld(t *testing.T) {
	s := newStore(t)
	c := humanConfirmation()
	if _, _, err := s.Promote(passingRun("run-1"), c); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	c.Supersede = true
	for _, id := range []string{"run-2", "run-1", "run-3"} {
		if _, _, err := s.Promote(passingRun(id), c); err != nil {
			t.Fatalf("supersede with %s: %v", id, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(s.SafeSpotsDir, "archive", "thing-flow"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	want := []string{"run-1-2.json", "run-1.json", "run-2.json"}
	if len(names) != len(want) {
		t.Fatalf("archive holds %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("archive holds %v, want %v: each file is named by the run id that supersedes names, and a run archived twice keeps both", names, want)
		}
	}
}
