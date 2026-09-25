package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLatencyConfig(t *testing.T, body string) string {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, DirName, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestConfigReadsALatencyBlock(t *testing.T) {
	dir := writeLatencyConfig(t, "target:\n  base_url: http://x\nlatency:\n  floor_ms: 100\n  ratio: 2.5\n  remeasure: 3\n  fail: true\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Latency == nil || *cfg.Latency.FloorMS != 100 || *cfg.Latency.Ratio != 2.5 || *cfg.Latency.Remeasure != 3 || !cfg.Latency.Fail {
		t.Fatalf("latency block not read: %+v", cfg.Latency)
	}
}

func TestConfigRefusesARatioBelowOne(t *testing.T) {
	dir := writeLatencyConfig(t, "target:\n  base_url: http://x\nlatency:\n  ratio: 0.5\n")
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "latency.ratio") {
		t.Fatalf("want a latency.ratio error, got %v", err)
	}
}
