package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForceRefreshesTheKitWithoutRewritingTheAdopterConfig(t *testing.T) {
	dir := t.TempDir()
	restore := chdir(t, dir)
	defer restore()

	if err := os.MkdirAll(filepath.Join(dir, ".shrt"), 0o755); err != nil {
		t.Fatal(err)
	}
	declared := "target:\n    base_url: https://backend.example\n" +
		"descriptor:\n    file: .shrt/descriptor.binpb\n" +
		"conventions:\n    item_envelope_path: results[].error.code\n" +
		"paths:\n    chains: .shrt/chains\n    runs: .shrt/runs\n    safespots: .shrt/safespots\n"
	cfg := filepath.Join(dir, ".shrt", "config.yaml")
	if err := os.WriteFile(cfg, []byte(declared), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit(t.Context(), []string{"-force", "-build=false", "-agents=false"}); err != nil {
		t.Fatalf("init -force: %v", err)
	}

	got, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "item_envelope_path") {
		t.Fatalf("init -force deleted a declared convention. It is the documented way to refresh the "+
			"installed docs, so this silently disarms a per-item verdict and turns a red chain green:\n%s", got)
	}
	if !strings.Contains(string(got), "backend.example") {
		t.Errorf("init -force reverted base_url to the default:\n%s", got)
	}
}

func TestForceConfigStillRebuildsWhenAskedExplicitly(t *testing.T) {
	dir := t.TempDir()
	restore := chdir(t, dir)
	defer restore()

	if err := os.MkdirAll(filepath.Join(dir, ".shrt"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, ".shrt", "config.yaml")
	if err := os.WriteFile(cfg, []byte("target:\n    base_url: https://old.example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runInit(t.Context(), []string{"-force-config", "-base-url", "https://new.example", "-build=false", "-agents=false"}); err != nil {
		t.Fatalf("init -force-config: %v", err)
	}
	got, _ := os.ReadFile(cfg)
	if !strings.Contains(string(got), "new.example") {
		t.Errorf("-force-config must still rebuild the config when asked for explicitly:\n%s", got)
	}
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() { _ = os.Chdir(prev) }
}
