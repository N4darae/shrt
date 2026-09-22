package config_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestNeverCommitNamesTheTokenCache(t *testing.T) {
	got := config.Default().NeverCommit()

	want := ".shrt/" + config.TokensFile
	for _, line := range got {
		if line == want {
			return
		}
	}
	t.Fatalf("%q is missing from %v. It holds live bearer tokens, and docs/importing.md's commit "+
		"checklist never named it, so the default path for a new adopter ended with credentials in "+
		"git history", want, got)
}

func TestNeverCommitCoversTheBuildOutput(t *testing.T) {
	got := strings.Join(config.Default().NeverCommit(), "\n")

	for _, want := range []string{".shrt/runs/", ".shrt/descriptor.binpb", ".shrt/docs/"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is build output and belongs in the ignore list: %v", want, got)
		}
	}
}

func TestNeverCommitFollowsARelocatedRunsDirectory(t *testing.T) {
	cfg := config.Default()
	cfg.Paths.Runs = "artifacts/shrt-runs"
	cfg.Descriptor.File = "build/descriptor.binpb"

	got := strings.Join(cfg.NeverCommit(), "\n")

	if !strings.Contains(got, "artifacts/shrt-runs/") {
		t.Errorf("paths: is configurable, so the ignore list has to read it rather than assume: %v", got)
	}
	if !strings.Contains(got, "build/descriptor.binpb") {
		t.Errorf("the descriptor can be relocated too: %v", got)
	}
	if strings.Contains(got, ".shrt/runs/") {
		t.Errorf("the default should not be emitted alongside the configured one: %v", got)
	}
}
