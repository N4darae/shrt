package main

import (
	"strings"
	"testing"
)

func TestEveryKeyAndFieldIsDocumented(t *testing.T) {
	out, err := render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(string(out), "UNDOCUMENTED") {
		t.Fatalf("GRAMMAR.md would ship an undocumented row")
	}
}

func TestRenderFailsOnAnUndocumentedField(t *testing.T) {
	const key = "StepRecord.body_refs"
	saved, had := notes[key]
	delete(notes, key)
	t.Cleanup(func() {
		if had {
			notes[key] = saved
		}
	})
	if _, err := render(); err == nil || !strings.Contains(err.Error(), key) {
		t.Fatalf("render must refuse a field with no note, naming %s: got %v", key, err)
	}
}
