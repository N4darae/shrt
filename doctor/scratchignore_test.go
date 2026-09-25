package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/doctor"
)

func ignoredLine(t *testing.T, r *doctor.Report) doctor.Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Check == doctor.CheckIgnored {
			return f
		}
	}
	t.Fatal("no gitignore finding")
	return doctor.Finding{}
}

func TestDoctorSuggestsIgnoringTheScratchDirWithoutFailing(t *testing.T) {
	cfg := repo(t)
	write(t, filepath.Join(cfg.Root, ".gitignore"), strings.Join(config.Default().NeverCommit(), "\n")+"\n")
	got := ignoredLine(t, run(t, cfg, options()))
	if got.Level != doctor.LevelOK {
		t.Fatalf("an unignored scratch dir is a suggestion, not a warning a strict gate fails on: %s %s", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail+got.Remedy, config.ScratchDir) {
		t.Fatalf("suggest ignoring %s, where the README writes exploratory slices: %s / %s", config.ScratchDir, got.Detail, got.Remedy)
	}

	write(t, filepath.Join(cfg.Root, ".gitignore"), strings.Join(config.Default().NeverCommit(), "\n")+"\n"+config.ScratchDir+"\n")
	got = ignoredLine(t, run(t, cfg, options()))
	if got.Level != doctor.LevelOK || strings.Contains(got.Detail, "not ignored") {
		t.Fatalf("an ignored scratch dir is accepted without a suggestion: %s %s", got.Level, got.Detail)
	}
}
