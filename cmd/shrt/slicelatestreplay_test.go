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

func TestSliceRunLatestIsTheNewerReplayWhenOnlyItFailedTheStep(t *testing.T) {
	_, e, base := approvedThingFlowRun(t)
	replay := copyRun(t, base, "29990101T000000Z-replay03")
	replay.ReplayOf = base.RunID
	for _, st := range replay.Steps {
		if st.ID == "fetch" {
			st.Status = runner.StatusFailed
		}
	}
	if _, err := e.store.SaveRun(replay); err != nil {
		t.Fatal(err)
	}
	var picked string
	note := captureStderr(t, func() {
		rec, err := loadRunReaching(e, "cli-thing-flow", "cli-thing-flow", "latest", "fetch")
		if err != nil {
			t.Fatal(err)
		}
		picked = rec.RunID
	})
	if picked != replay.RunID {
		t.Fatalf("-run latest picked %s, want the replay in which fetch failed, %s", picked, replay.RunID)
	}
	if !strings.Contains(note, "run 29990101T000000Z-replay03, the newest record, a `shrt verify` replay in which fetch failed") ||
		!strings.Contains(note, base.RunID+", passed it") {
		t.Fatalf("the note names the run picked and the one passed over: %q", note)
	}
}

func TestSliceRunLatestIsTheNewestReplayWhenNoRunWasRecordedBesideIt(t *testing.T) {
	_, e, base := approvedThingFlowRun(t)
	for _, id := range []string{"29990101T000000Z-replay04", "29990101T000001Z-replay05"} {
		replay := copyRun(t, base, id)
		replay.ReplayOf = base.RunID
		if _, err := e.store.SaveRun(replay); err != nil {
			t.Fatal(err)
		}
	}
	var picked string
	note := captureStderr(t, func() {
		rec, err := loadRunReaching(e, "cli-thing-flow", "cli-thing-flow", "latest", "fetch")
		if err != nil {
			t.Fatal(err)
		}
		picked = rec.RunID
	})
	if picked != "29990101T000001Z-replay05" {
		t.Fatalf("-run latest picked %s, want the newest replay, as shrt diff compares it", picked)
	}
	if !strings.Contains(note, "as shrt diff picks it") || !strings.Contains(note, "-run "+base.RunID) {
		t.Fatalf("the note names the replay picked and the run passed over: %q", note)
	}
}

func TestSliceWithoutRunLatestIsTheNewerReplayWhenOnlyItFailedSteps(t *testing.T) {
	_, e, base := approvedThingFlowRun(t)
	replay := copyRun(t, base, "29990101T000000Z-replay06")
	replay.ReplayOf = base.RunID
	for _, st := range replay.Steps {
		if st.ID == "fetch" {
			st.Status = runner.StatusFailed
		}
	}
	if _, err := e.store.SaveRun(replay); err != nil {
		t.Fatal(err)
	}
	var picked string
	note := captureStderr(t, func() {
		rec, err := latestRun(e, "cli-thing-flow", "")
		if err != nil {
			t.Fatal(err)
		}
		picked = rec.RunID
	})
	if picked != replay.RunID {
		t.Fatalf("-without -run latest picked %s, want the replay in which a step failed, %s", picked, replay.RunID)
	}
	if !strings.Contains(note, "replay in which 1 step failed") || !strings.Contains(note, base.RunID+", no step failed") {
		t.Fatalf("the note names the run picked and the one passed over: %q", note)
	}
	if got := newerFailing(e, base); got == nil || got.RunID != replay.RunID {
		t.Fatalf("newerFailing(%s) = %v, want the replay %s", base.RunID, got, replay.RunID)
	}
}

func TestSliceWithoutVerifyRunLatestComparesTheReplayThatFailed(t *testing.T) {
	stockWorkspace(t, 0)
	e, err := loadEnv(true)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := e.store.LatestRun("stock")
	if err != nil {
		t.Fatal(err)
	}
	own := copyRun(t, failed, "29990101T000000Z-own00001")
	own.Status = runner.StatusPassed
	for _, st := range own.Steps {
		st.Status = runner.StatusPassed
	}
	replay := copyRun(t, failed, "29990101T000001Z-replay07")
	replay.ReplayOf = own.RunID
	for _, rec := range []*runner.Record{own, replay} {
		if _, err := e.store.SaveRun(rec); err != nil {
			t.Fatal(err)
		}
	}
	out, _ := stockWithout(t, "-without", "stray_add", "-verify", "-run", "latest")
	if !strings.Contains(out, "failed in source run "+replay.RunID) {
		t.Fatalf("-run latest compares the replay in which steps failed:\n%s", out)
	}
	out, _ = stockWithout(t, "-without", "stray_add", "-verify", "-run", own.RunID)
	want := "no step left in failed in source run " + own.RunID + " (a `shrt run` record) to compare; in the newer record " + replay.RunID + ", a `shrt verify` replay, 3 steps failed: pass -run " + replay.RunID
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}
