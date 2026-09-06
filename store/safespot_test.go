package store_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	root := t.TempDir()
	return store.New(filepath.Join(root, "runs"), filepath.Join(root, "safespots"))
}

func passingRun(id string) *runner.Record {
	return &runner.Record{
		RunID: id, Chain: "thing-flow", Target: "http://localhost", Status: runner.StatusPassed,
		Steps: []*runner.StepRecord{{
			Index: 1, ID: "create", Call: "ThingService/Create", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"id":"thing-1"}`),
		}},
	}
}

func humanConfirmation() store.Confirmation {
	return store.Confirmation{By: "reviewer", Acknowledged: true, Note: "checked in the admin UI"}
}

func TestPromoteRequiresExplicitHumanConfirmation(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-1")

	cases := map[string]store.Confirmation{
		"no confirmer":       {Acknowledged: true},
		"no acknowledgement": {By: "reviewer"},
		"nothing at all":     {},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := s.Promote(rec, c); !errors.Is(err, store.ErrNotConfirmed) {
				t.Fatalf("want ErrNotConfirmed, got %v", err)
			}
			if s.HasSafeSpot(rec.Chain) {
				t.Fatal("a rejected confirmation must not write a safe spot")
			}
		})
	}
}

func TestPromoteRefusesAFailedRun(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-1")
	rec.Status = runner.StatusFailed

	if _, _, err := s.Promote(rec, humanConfirmation()); !errors.Is(err, store.ErrRunNotPassed) {
		t.Fatalf("want ErrRunNotPassed, got %v", err)
	}
}

func TestPromoteWillNotSilentlyOverwriteAnExistingSafeSpot(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.Promote(passingRun("run-1"), humanConfirmation()); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	if _, _, err := s.Promote(passingRun("run-2"), humanConfirmation()); !errors.Is(err, store.ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	spot, err := s.LoadSafeSpot("thing-flow")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if spot.RunID != "run-1" {
		t.Fatalf("the original safe spot must survive, got run %s", spot.RunID)
	}
}

func TestSupersedeArchivesThePreviousSafeSpot(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.Promote(passingRun("run-1"), humanConfirmation()); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	c := humanConfirmation()
	c.Supersede = true
	spot, _, err := s.Promote(passingRun("run-2"), c)
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if spot.RunID != "run-2" || spot.Supersedes != "run-1" {
		t.Fatalf("want run-2 superseding run-1, got %s superseding %s", spot.RunID, spot.Supersedes)
	}
	archived, err := filepath.Glob(filepath.Join(s.SafeSpotsDir, "archive", "thing-flow", "*.json"))
	if err != nil || len(archived) != 1 {
		t.Fatalf("want 1 archived safe spot, got %v (%v)", archived, err)
	}
}

func TestRunRoundTrip(t *testing.T) {
	s := newStore(t)
	if _, err := s.SaveRun(passingRun("run-1")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := s.SaveRun(passingRun("run-2")); err != nil {
		t.Fatalf("save: %v", err)
	}
	latest, err := s.LoadRun("thing-flow", "latest")
	if err != nil {
		t.Fatalf("load latest: %v", err)
	}
	if latest.RunID != "run-2" {
		t.Fatalf("want run-2 as latest, got %s", latest.RunID)
	}
}
