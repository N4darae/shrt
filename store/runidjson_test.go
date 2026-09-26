package store_test

import (
	"testing"
)

func TestARunIDWithATrailingJSONExtensionIsAccepted(t *testing.T) {
	s := newStore(t)
	rec := passingRun("20260924T205643Z-8800eb1b")
	if _, err := s.SaveRun(rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadRun(rec.Chain, rec.RunID+".json")
	if err != nil || got.RunID != rec.RunID {
		t.Fatalf("a run id pasted with its file extension names the same run: %v", err)
	}
	found, err := s.FindRun(rec.RunID + ".json")
	if err != nil || len(found) != 1 {
		t.Fatalf("FindRun must accept the file name too: %v %d", err, len(found))
	}
}
