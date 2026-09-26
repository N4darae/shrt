package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/agentkit"
)

func TestDoctorFailsOnADriftedAgentKit(t *testing.T) {
	root := adoptedRepo(t)
	if _, err := agentkit.Install(root, agentkit.ClaudeAssets(), true); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(root, ".claude", "skills", "shrt", "SKILL.md")
	if err := os.WriteFile(skill, []byte("a skill from an older binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() { _ = runDoctor(context.Background(), nil) })
	if !strings.Contains(out, ".claude/skills/shrt/SKILL.md") || !strings.Contains(out, "drifted") {
		t.Fatalf("an agent follows the installed skill, so a skill that disagrees with the binary must be "+
			"reported like drifted docs:\n%s", out)
	}
	if code := doctorExitCode(t); code != 1 {
		t.Fatalf("a drifted agent kit is a failure like drifted docs, want exit 1, got %d", code)
	}
}

func TestDoctorPassesAMatchingAgentKitAndToleratesNone(t *testing.T) {
	root := adoptedRepo(t)
	out := captureStdout(t, func() { _ = runDoctor(context.Background(), nil) })
	if strings.Contains(out, "FAIL  agentkit") {
		t.Fatalf("init -agents=false installs no kit, which is a choice, not a failure:\n%s", out)
	}
	if _, err := agentkit.Install(root, agentkit.ClaudeAssets(), true); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() { _ = runDoctor(context.Background(), nil) })
	if !strings.Contains(out, "agent kit") || strings.Contains(out, "FAIL  agentkit") {
		t.Fatalf("a freshly installed kit matches the binary and must say so:\n%s", out)
	}
}
