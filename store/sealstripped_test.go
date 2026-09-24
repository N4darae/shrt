package store_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestARecordWhoseSealWasDeletedIsRefusedAsEdited(t *testing.T) {
	s := newStore(t)
	var notes strings.Builder
	s.Notes = &notes
	rec := passingRun("run-stripped")
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
	doc := map[string]any{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["seal"]; !ok {
		t.Fatalf("saved record has no seal:\n%s", raw)
	}
	delete(doc, "seal")
	doc["status"] = runner.StatusPassed
	doc["steps"].([]any)[0].(map[string]any)["status"] = runner.StatusPassed
	edited, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.LoadRun(rec.Chain, rec.RunID)
	if !errors.Is(err, store.ErrRunEdited) || !strings.Contains(err.Error(), "seal was removed") {
		t.Fatalf("a record written by a sealing build whose seal is gone was edited, not old; got %v (notes %q)", err, notes.String())
	}
	if strings.Contains(notes.String(), "predates") {
		t.Fatalf("a stripped seal must not be reported as predating seals: %q", notes.String())
	}
	loaded := &runner.Record{}
	if err := json.Unmarshal(edited, loaded); err != nil {
		t.Fatal(err)
	}
	if err := store.SealState(loaded); !errors.Is(err, store.ErrRunEdited) {
		t.Fatalf("SealState must call a stripped record edited; got %v", err)
	}
	if _, err := s.Propose(loaded, store.ProposalInput{Checked: "looks right"}); !errors.Is(err, store.ErrRunEdited) {
		t.Fatalf("a stripped record cannot be proposed as edited; got %v", err)
	}
}
