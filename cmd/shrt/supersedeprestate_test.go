package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/store"
)

func TestSupersedeDoesNotCallAnIntendedChangeUnstableAgainstARunFromBeforeIt(t *testing.T) {
	spotRun := echoRecord(t, "r0",
		[3]any{"create", map[string]any{"name": "other"}, map[string]any{"name": "other", "seq": "0"}})
	prev := echoRecord(t, "r1",
		[3]any{"create", map[string]any{"name": "other"}, map[string]any{"name": "other", "note": "", "seq": "1"}},
		[3]any{"get_customer", map[string]any{"name": "other"}, map[string]any{"name": "other"}})
	rec := echoRecord(t, "r2",
		[3]any{"create", map[string]any{"name": "other"}, map[string]any{"name": "other", "note": "gift wrap", "seq": "2"}})
	spot := &store.SafeSpot{Chain: "echo", RunID: "r0", Steps: spotRun.Steps}
	unsent := func(_, path string, v any) bool { return path == "note" && v == "" }
	unstable, carried := unstableAgainstSpot(prev, rec, spot, nil, nil, unsent)
	if strings.Join(unstable, ",") != "create seq: 1 -> 2" {
		t.Fatalf("only a field that differs between two runs of the same backend state is unstable; a step absent in one run is not, got %v", unstable)
	}
	if strings.Join(carried, ",") != `create note: "" -> gift wrap` {
		t.Fatalf("a field where the earlier run still holds the replaced safe spot's value is the intended change, got %v", carried)
	}
}

func TestSupersedeSummaryExplainsFieldsCarriedFromBeforeTheChange(t *testing.T) {
	p := &store.Proposal{Chain: "echo", RunID: "r2", Replaces: "r0", ComparedTo: "r1",
		Carried: []string{"create note:  -> gift wrap"}}
	rec := echoRecord(t, "r2", [3]any{"create", map[string]any{"name": "other"}, map[string]any{"note": "gift wrap"}})
	out := store.ProposalSummary(p, rec)
	if strings.Contains(out, "not declared volatile") {
		t.Fatalf("no volatile advice for an intended change:\n%s", out)
	}
	if !strings.Contains(out, "still held what the safe spot it replaces holds") {
		t.Fatalf("the summary should say the earlier run predates the change:\n%s", out)
	}
}
