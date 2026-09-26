package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestSliceRunLatestPrefersTheChainsOwnRunOverAVerifyReplay(t *testing.T) {
	_, e, base := approvedThingFlowRun(t)
	replay := copyRun(t, base, "29990101T000000Z-replay01")
	replay.ReplayOf = base.RunID
	if _, err := e.store.SaveRun(replay); err != nil {
		t.Fatal(err)
	}
	var err error
	var picked string
	note := captureStderr(t, func() {
		rec, loadErr := loadRunReaching(e, "cli-thing-flow", "cli-thing-flow", "latest", "fetch")
		err = loadErr
		if rec != nil {
			picked = rec.RunID
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if picked != base.RunID {
		t.Fatalf("-run latest picked %s, want the shrt run record %s over the verify replay", picked, base.RunID)
	}
	for _, want := range []string{"the newest `shrt run` record", "29990101T000000Z-replay01, is a `shrt verify` replay", "pass -run 29990101T000000Z-replay01"} {
		if !strings.Contains(note, want) {
			t.Errorf("missing %q in %q", want, note)
		}
	}
}

func TestSliceRunLatestIsTheNewerReplayWhenTheRunsDifferInReach(t *testing.T) {
	_, e, base := approvedThingFlowRun(t)
	replay := copyRun(t, base, "29990101T000000Z-replay02")
	replay.ReplayOf = base.RunID
	for _, st := range replay.Steps {
		if st.ID == "fetch" {
			st.Status = runner.StatusSkipped
		}
	}
	if _, err := e.store.SaveRun(replay); err != nil {
		t.Fatal(err)
	}
	var err error
	note := captureStderr(t, func() {
		_, err = loadRunReaching(e, "cli-thing-flow", "cli-thing-flow", "latest", "fetch")
	})
	if exitCodeOf(err) != 3 || !strings.Contains(err.Error(), "run 29990101T000000Z-replay02, which did not evaluate step fetch") ||
		!strings.Contains(err.Error(), "-run "+base.RunID) {
		t.Fatalf("the newest record did not reach fetch and the run record did: no silent pick of the older one, got %v", err)
	}
	if note != "" {
		t.Fatalf("a refusal needs no note: %q", note)
	}
}
