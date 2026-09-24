package store_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestAHandEditedRunRecordCannotBeProposed(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-edit")
	rec.Status = runner.StatusFailed
	rec.Steps[0].Status = runner.StatusFailed
	path, err := s.SaveRun(rec)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.ReplaceAll(string(raw), `"status": "failed"`, `"status": "passed"`)
	if edited == string(raw) {
		t.Fatalf("the test did not edit anything:\n%s", raw)
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRun(rec.Chain, rec.RunID); !errors.Is(err, store.ErrRunEdited) {
		t.Fatalf("a run record edited after shrt wrote it is not loaded as evidence; got %v", err)
	}
	loaded := &runner.Record{}
	if err := json.Unmarshal([]byte(edited), loaded); err != nil {
		t.Fatal(err)
	}
	if !loaded.Passed() {
		t.Fatal("the edit should make the record read as passed")
	}
	_, err = s.Propose(loaded, store.ProposalInput{Checked: "looks right"})
	if !errors.Is(err, store.ErrRunEdited) {
		t.Fatalf("a run record edited after shrt wrote it is not what ran, so it cannot be proposed; got %v", err)
	}
}

func TestAnUnsealedRunRecordCannotBeProposed(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-legacy")
	dir := filepath.Join(s.RunsDir, "thing-flow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run-legacy.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.LoadRun(rec.Chain, rec.RunID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Propose(loaded, store.ProposalInput{Checked: "looks right"})
	if !errors.Is(err, store.ErrRunUnsealed) || !strings.Contains(strings.ToLower(err.Error()), "run the chain again") {
		t.Fatalf("a record with no seal cannot be checked for edits, so it is refused with a way out; got %v", err)
	}
	if !strings.HasPrefix(err.Error(), "the run record predates sealed run records") || strings.Contains(err.Error(), "not the one shrt wrote") {
		t.Fatalf("an unsealed record is refused first as predating seals, not as tampered; got %v", err)
	}
}

func TestAnUnsealedRunRecordIsLoadedWithOneNote(t *testing.T) {
	s := newStore(t)
	var notes strings.Builder
	s.Notes = &notes
	dir := filepath.Join(s.RunsDir, "thing-flow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"run-old-1", "run-old-2"} {
		raw, err := json.Marshal(passingRun(id))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadRun("thing-flow", id); err != nil {
			t.Fatalf("an unsealed record is still read, with a note: %v", err)
		}
	}
	if strings.Count(notes.String(), "\n") != 1 || !strings.Contains(notes.String(), "predates sealed run records") {
		t.Fatalf("one line must say the record predates seals, got %q", notes.String())
	}
}

func TestASavedRunRecordIsSealedAndProposable(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-ok")
	rec.Vars = map[string]any{"n": 3, "big": json.Number("12345678901234567890"), "s": "<a&b>"}
	rec.Exports = map[string]any{"f": 1.5}
	if _, err := s.SaveRun(rec); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.LoadRun(rec.Chain, rec.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Seal == "" {
		t.Fatal("SaveRun must seal the record")
	}
	if _, err := s.Propose(loaded, store.ProposalInput{Checked: "checked"}); err != nil {
		t.Fatalf("an untouched record must stay proposable after a round trip through disk: %v", err)
	}
}

func TestAStrippedRunRecordDatedAfterSealsIsRefusedAsEdited(t *testing.T) {
	s := newStore(t)
	dir := filepath.Join(s.RunsDir, "thing-flow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id      string
		started time.Time
		want    error
	}{
		{"run-stripped", time.Date(2026, 9, 24, 22, 27, 47, 0, time.UTC), store.ErrRunEdited},
		{"run-older", time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC), nil},
	} {
		rec := passingRun(tc.id)
		rec.StartedAt = tc.started
		raw, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, tc.id+".json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err = s.LoadRun(rec.Chain, rec.RunID)
		if tc.want == nil && err != nil {
			t.Fatalf("%s started before seals, so it is loaded as predating them: %v", tc.id, err)
		}
		if tc.want != nil && (!errors.Is(err, tc.want) || !strings.Contains(err.Error(), "began sealing")) {
			t.Fatalf("%s started after every build sealed its records, so an unsealed copy was stripped: %v", tc.id, err)
		}
	}
}
