package store_test

import "testing"

func TestASafeSpotKeepsTheBuildItsRunWasStampedWith(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-1")
	rec.Build = "rc-3"
	if _, _, err := s.Promote(rec, humanConfirmation()); err != nil {
		t.Fatal(err)
	}
	spot, err := s.LoadSafeSpot(rec.Chain)
	if err != nil {
		t.Fatal(err)
	}
	if spot.Build != "rc-3" {
		t.Fatalf("safe spot build = %q, want rc-3: a replay is diffed against it, and a diff across two builds must say so", spot.Build)
	}
}
