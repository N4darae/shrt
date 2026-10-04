package config_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestConfigReadsALatencyBlock(t *testing.T) {
	dir := write(t, "target:\n  base_url: http://x\nlatency:\n  floor_ms: 100\n  ratio: 2.5\n  remeasure: 3\n  fail: true\n")
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Latency == nil || *cfg.Latency.FloorMS != 100 || *cfg.Latency.Ratio != 2.5 || *cfg.Latency.Remeasure != 3 || !cfg.Latency.Fail {
		t.Fatalf("latency block not read: %+v", cfg.Latency)
	}
}

func TestConfigRefusesARatioBelowOne(t *testing.T) {
	dir := write(t, "target:\n  base_url: http://x\nlatency:\n  ratio: 0.5\n")
	if _, err := config.Load(dir); err == nil || !strings.Contains(err.Error(), "latency.ratio") {
		t.Fatalf("want a latency.ratio error, got %v", err)
	}
}
