package doctor_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/doctor"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func findAll(r *doctor.Report, check string) []doctor.Finding {
	out := []doctor.Finding{}
	for _, f := range r.Findings {
		if f.Check == check {
			out = append(out, f)
		}
	}
	return out
}

func TestDoctorListsPreUpgradeArtefactsWithTheFixForEach(t *testing.T) {
	cfg := repo(t)
	cfg.Target.BaseURL = "http://new.example.test"
	s := store.New(cfg.Abs(cfg.Paths.Runs), cfg.Abs(cfg.Paths.SafeSpots))
	old := &runner.Record{RunID: "20260101T000000Z-00000001", Chain: "orders", Target: "http://old.example.test", Status: runner.StatusPassed,
		Steps: []*runner.StepRecord{{Index: 1, ID: "create", Call: "X/Create", AuthProfile: "default", Status: runner.StatusPassed}}}
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(s.RunsDir, "orders", old.RunID+".json"), string(raw))
	spot := &store.SafeSpot{Chain: "orders", RunID: old.RunID, Target: old.Target, Steps: old.Steps}
	spot.Digest = spot.ComputeDigest()
	raw, err = json.Marshal(spot)
	if err != nil {
		t.Fatal(err)
	}
	write(t, s.SafeSpotPath("orders"), string(raw))
	write(t, cfg.Abs(filepath.Join(config.DirName, config.TokensFile)),
		`{"default#aaaa":{"token":"t1","expires_at":"2099-01-01T00:00:00Z"},"default#bbbb":{"token":"t2","expires_at":"2099-01-01T00:00:00Z"},`+
			`"default#cccc":{"token":"t3","expires_at":"2099-01-01T00:00:00Z"}}`)

	opts := options()
	opts.TokenKeys = func(_ *config.Config, target string) []string {
		switch target {
		case "http://new.example.test":
			return []string{"default#aaaa"}
		case "http://old.example.test":
			return []string{"default#bbbb"}
		}
		return nil
	}
	r := run(t, cfg, opts)

	text := ""
	for _, f := range findAll(r, doctor.CheckUpgrade) {
		if f.Level != doctor.LevelWarn {
			t.Fatalf("a pre-upgrade artefact is a warning, got %s: %s", f.Level, f.Detail)
		}
		text += f.Detail + "\n" + f.Remedy + "\n"
	}
	for _, want := range []string{
		"predate sealed run records", "shrt run orders",
		"no auth_principal", "shrt confirm orders -supersede",
		"http://old.example.test", "1 cached token(s) minted against another base_url",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the upgrade findings must mention %q:\n%s", want, r.Text(true))
		}
	}
	tokens := ""
	for _, f := range findAll(r, doctor.CheckTokens) {
		tokens += f.Detail + "\n"
	}
	if !strings.Contains(tokens, "1 cached token(s) for this target's logins") || !strings.Contains(tokens, "1 minted against another base_url (http://old.example.test)") ||
		!strings.Contains(tokens, "1 for another login") {
		t.Fatalf("a cache entry no login of this config uses is counted apart from cached/expired:\n%s", tokens)
	}
}

func TestDoctorReportsNoPreUpgradeArtefactsInAFreshRepo(t *testing.T) {
	r := run(t, repo(t), options())
	for _, f := range findAll(r, doctor.CheckUpgrade) {
		if f.Level != doctor.LevelOK {
			t.Fatalf("a fresh repo has nothing from before the upgrade:\n%s", r.Text(true))
		}
	}
}
