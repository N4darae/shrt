package store_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/store"
)

func TestASealBypassedByEditingFormatAndSealIsRefusedAsEdited(t *testing.T) {
	edits := map[string]func(doc map[string]any){
		"format and seal removed": func(doc map[string]any) { delete(doc, "format"); delete(doc, "seal") },
		"format 0, seal removed":  func(doc map[string]any) { doc["format"] = 0; delete(doc, "seal") },
		"format null":             func(doc map[string]any) { doc["format"] = nil; delete(doc, "seal") },
		"format negative":         func(doc map[string]any) { doc["format"] = -2 },
		"format fractional":       func(doc map[string]any) { doc["format"] = 1.5 },
		"format text":             func(doc map[string]any) { doc["format"] = "2" },
		"empty seal, no format":   func(doc map[string]any) { delete(doc, "format"); doc["seal"] = "" },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			var notes strings.Builder
			s.Notes = &notes
			rec := passingRun("run-bypass")
			rec.Steps[0].BodyRefs = map[string]string{"id": "${create.id}"}
			rec.Steps[0].AuthPrincipal = "p-1"
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
			edit(doc)
			out, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, out, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err = s.LoadRun(rec.Chain, rec.RunID)
			if !errors.Is(err, store.ErrRunEdited) {
				t.Fatalf("a record carrying fields only a sealing build writes, with no valid seal or format, was edited; got %v (notes %q)", err, notes.String())
			}
			if strings.Contains(notes.String(), "predates") {
				t.Fatalf("must not be reported as predating seals: %q", notes.String())
			}
		})
	}
}

func TestARecordFromBeforeSealsIsStillUsedAsRecorded(t *testing.T) {
	s := newStore(t)
	var notes strings.Builder
	s.Notes = &notes
	rec := passingRun("run-old")
	path, err := s.SaveRun(rec)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	doc := map[string]any{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	delete(doc, "format")
	delete(doc, "seal")
	out, _ := json.Marshal(doc)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRun(rec.Chain, rec.RunID); err != nil {
		t.Fatalf("a record with nothing only a sealing build writes predates seals: %v", err)
	}
}
