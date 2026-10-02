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

func TestTheReferencesSectionSaysWhichSpellingToWrite(t *testing.T) {
	out, err := render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_, refs, _ := strings.Cut(string(out), "## 2. References")
	refs, _, _ = strings.Cut(refs, "## 3.")
	for _, want := range []string{"Write `${<step>.<path>}`", "`${steps.<step>.request.<field>}`", "`contract plan` and `chain new`"} {
		if !strings.Contains(refs, want) {
			t.Errorf("GRAMMAR §2 must recommend the form shrt writes, %q:\n%s", want, refs)
		}
	}
}
