package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/N4darae/shrt/runner"
)

func approvedSpot() *SafeSpot {
	at := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	spot := &SafeSpot{Chain: "c", RunID: "r", Target: "t", ConfirmedBy: "alice@example.test", ConfirmedAt: at, Note: "checked", ProposedBy: "agent", ProposedAt: &at,
		Steps: []*runner.StepRecord{{ID: "a", Call: "S/A", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)}}}
	spot.Digest = spot.ComputeDigest()
	return spot
}

func TestSafeSpotDigestCoversTheApprovalIdentity(t *testing.T) {
	edits := map[string]func(*SafeSpot){
		"confirmed_by": func(s *SafeSpot) { s.ConfirmedBy = "mallory@example.test" },
		"confirmed_at": func(s *SafeSpot) { s.ConfirmedAt = s.ConfirmedAt.Add(time.Hour) },
		"note":         func(s *SafeSpot) { s.Note = "rubber stamp" },
		"proposed_by":  func(s *SafeSpot) { s.ProposedBy = "someone" },
		"supersedes":   func(s *SafeSpot) { s.Supersedes = "r0" },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			spot := approvedSpot()
			if spot.DigestKind() != DigestCurrent {
				t.Fatalf("a freshly sealed spot must be of the current kind, got %q", spot.DigestKind())
			}
			edit(spot)
			if spot.DigestMatches() {
				t.Fatalf("editing %s after approval must not match", name)
			}
		})
	}
}

func TestSafeSpotSealedBeforeTheApprovalWasCoveredStillLoads(t *testing.T) {
	spot := approvedSpot()
	spot.Digest = hashJSON(struct {
		Chain, RunID, Target, Build string
		Volatile                    []string
		Steps                       []*runner.StepRecord
	}{spot.Chain, spot.RunID, spot.Target, spot.Build, spot.Volatile, spot.Steps})
	if !spot.DigestMatches() || spot.DigestKind() != DigestWithoutApproval {
		t.Fatalf("a spot sealed before the digest covered the approval must load and say so, got %q", spot.DigestKind())
	}
	spot.Steps[0].Response = json.RawMessage(`{"n":2}`)
	if spot.DigestMatches() {
		t.Fatal("an edited response in an older spot must not match")
	}
}
