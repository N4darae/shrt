package store

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestSafeSpotDigestCatchesAnyEditAndAcceptsOlderSpots(t *testing.T) {
	spot := &SafeSpot{Chain: "c", RunID: "r", Target: "t", Volatile: []string{"**.sku"}, Steps: []*runner.StepRecord{
		{ID: "a", Call: "S/A", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)},
	}}
	spot.Digest = spot.ComputeDigest()
	if !spot.DigestMatches() {
		t.Fatal("a sealed safe spot must match")
	}
	spot.Volatile = append(spot.Volatile, "**")
	if spot.DigestMatches() {
		t.Fatal("an edited volatile list must not match")
	}
	spot.Volatile = spot.Volatile[:1]
	spot.Steps[0].Status = runner.StatusFailed
	if spot.DigestMatches() {
		t.Fatal("an edited step status must not match")
	}
	legacy := &SafeSpot{Chain: "c", Steps: []*runner.StepRecord{{ID: "a", Call: "S/A", Response: json.RawMessage(`{"n":1}`)}}}
	legacy.Digest = legacyDigest(legacy.Steps)
	if !legacy.DigestMatches() {
		t.Fatal("a safe spot approved before the digest covered everything must still load")
	}
	legacy.Steps[0].Response = json.RawMessage(`{"n":2}`)
	if legacy.DigestMatches() {
		t.Fatal("an older safe spot with an edited response must not match")
	}
}
