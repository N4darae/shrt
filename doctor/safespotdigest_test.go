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
		t.Fatalf("a sealed safe spot is ok:\n%s", r.Text())
	}
	edited := sealedSpot(t, "edited")
	edited.ConfirmedBy = "mallory@example.test"
	write(t, filepath.Join(dir, "edited.json"), spotJSON(t, edited))
	r = run(t, cfg, options())
	f := find(t, r, doctor.CheckSafeSpotDigests)
	if f.Level != doctor.LevelError || !strings.Contains(f.Detail, "edited.json") || strings.Contains(f.Detail, "good.json") {
		t.Fatalf("a safe spot edited after approval FAILs, naming only it:\n%s", r.Text())
	}
}
