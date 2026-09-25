package main

import (
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestInitWritesLatencyFailIntoANewConfigOnly(t *testing.T) {
	dir := t.TempDir()
	defer chdir(t, dir)()
	captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Errorf("init: %v", err)
		}
	})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Latency == nil || !cfg.Latency.Fail {
		raw, _ := os.ReadFile(".shrt/config.yaml")
		t.Fatalf("a new config must fail a confirmed slowdown, so the documented gate is red on one:\n%s", raw)
	}
	writeFile(t, ".shrt/config.yaml", "target:\n    base_url: http://127.0.0.1:1\ndescriptor:\n    file: .shrt/descriptor.binpb\n")
	captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Errorf("init again: %v", err)
		}
	})
	raw, _ := os.ReadFile(".shrt/config.yaml")
	if strings.Contains(string(raw), "latency") {
		t.Fatalf("an existing config is the user's: init must not add latency to it:\n%s", raw)
	}
}
