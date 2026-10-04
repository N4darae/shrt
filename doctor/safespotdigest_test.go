package doctor_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/doctor"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func sealedSpot(t *testing.T, chainName string) *store.SafeSpot {
	t.Helper()
	spot := &store.SafeSpot{Chain: chainName, RunID: "r1", ConfirmedBy: "alice@example.test",
		Steps: []*runner.StepRecord{{Index: 1, ID: "create", Call: "shrt.test.v1.ThingService/Create", Status: runner.StatusPassed}}}
	spot.Digest = spot.ComputeDigest()
	return spot
}

func spotJSON(t *testing.T, spot *store.SafeSpot) string {
	t.Helper()
	raw, err := json.MarshalIndent(spot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestDoctorFailsASafeSpotWhoseDigestNoLongerMatches(t *testing.T) {
	cfg := repo(t)
	dir := cfg.Abs(cfg.Paths.SafeSpots)
	write(t, filepath.Join(dir, "good.json"), spotJSON(t, sealedSpot(t, "good")))
	r := run(t, cfg, options())
	if f := find(t, r, doctor.CheckSafeSpotDigests); f.Level != doctor.LevelOK {
		t.Fatalf("a sealed safe spot is ok:\n%s", r.Text(true))
	}
	edited := sealedSpot(t, "edited")
	edited.ConfirmedBy = "mallory@example.test"
	write(t, filepath.Join(dir, "edited.json"), spotJSON(t, edited))
	r = run(t, cfg, options())
	f := find(t, r, doctor.CheckSafeSpotDigests)
	if f.Level != doctor.LevelError || !strings.Contains(f.Detail, "edited.json") || strings.Contains(f.Detail, "good.json") {
		t.Fatalf("a safe spot edited after approval FAILs, naming only it:\n%s", r.Text(true))
	}
}

func TestDoctorFailsASafeSpotLeftWithMergeConflictMarkers(t *testing.T) {
	cfg := repo(t)
	ours, theirs := spotJSON(t, sealedSpot(t, "merged")), spotJSON(t, sealedSpot(t, "merged"))
	conflicted := "<<<<<<< HEAD\n" + ours + "\n=======\n" + theirs + "\n>>>>>>> feat/other\n"
	write(t, filepath.Join(cfg.Abs(cfg.Paths.SafeSpots), "merged.json"), conflicted)
	r := run(t, cfg, options())
	var hit *doctor.Finding
	for _, g := range findAll(r, doctor.CheckSafeSpotDigests) {
		if strings.Contains(g.Detail, "merge conflict") {
			g := g
			hit = &g
		}
	}
	if hit == nil || hit.Level != doctor.LevelError {
		t.Fatalf("conflict markers in a safe spot FAIL and say so:\n%s", r.Text(true))
	}
	if !strings.Contains(hit.Remedy, "--ours") || !strings.Contains(hit.Remedy, "git history") {
		t.Fatalf("the remedy says to take one side and where the other approval stays: %s", hit.Remedy)
	}
	s := store.New(cfg.Abs(cfg.Paths.Runs), cfg.Abs(cfg.Paths.SafeSpots))
	if _, err := s.LoadSafeSpot("merged"); err == nil || !strings.Contains(err.Error(), "merge conflict") {
		t.Fatalf("verify loads the safe spot through the store, which must name the conflict: %v", err)
	}
}
