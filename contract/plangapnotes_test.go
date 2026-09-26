package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestGapNotesKeepWhatCouldNotBePlannedAndDropTheWhy(t *testing.T) {
	p := &contract.Plan{Notes: []string{
		"step cancel: a fresh record moves first and the read after it holds the new state; the contract says nothing about what Cancel on a held record gives back, so the other reads assert only that they answer",
		"step create: the duplicate attempts assert only that the call was refused",
		"step create: create_replay replays the key and must return the first id",
		"step create: name is required and has no usable value: set fields.name.value",
		"Batch touches what the plan tracks and its contract says neither what it does to it nor that it leaves it alone",
	}}
	got := p.GapNotes()
	want := []string{
		"step cancel: the contract says nothing about what Cancel on a held record gives back, so the other reads assert only that they answer",
		"step create: the duplicate attempts assert only that the call was refused",
		"Batch touches what the plan tracks and its contract says neither what it does to it nor that it leaves it alone",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d gap notes, want %d:\n%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("gap %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}
