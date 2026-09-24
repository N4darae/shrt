package runner

import (
	"strings"
	"testing"
)

func TestSeedingNoteForARefusedLoginIsOneSentenceListingEachProfile(t *testing.T) {
	cases := []seeding{
		{tokenless: []string{"clerk", "default"}},
		{refused: []string{"clerk"}, tokenless: []string{"default"}},
	}
	for _, s := range cases {
		note := seedingNote(s)
		if strings.Count(note, "did not seed") != 1 || strings.Contains(note, "\n") {
			t.Errorf("%+v: want one did-not-seed sentence, got %q", s, note)
		}
		if !strings.Contains(note, "(clerk, shared (default))") {
			t.Errorf("%+v: want both profiles listed once, got %q", s, note)
		}
	}
}

func TestSeedingNoteNamesEveryOtherProfileOnTheRpcWhenOneWasSeeded(t *testing.T) {
	note := seedingNote(seeding{seeded: []string{"clerk"}, refused: []string{"default", "ops"}, tokenless: nil})
	if !strings.Contains(note, "the ops, shared (default) profiles on the same login rpc were not seeded") {
		t.Fatalf("note = %q", note)
	}
	if strings.Contains(note, "did not seed") {
		t.Fatalf("a seeded login must not read as a failed one: %q", note)
	}
}
