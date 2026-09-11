package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestLoadRejectsUnknownTargetKey(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, config.DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "target:\n    base_url: http://x\n    host_overide: y\n"
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(root); err == nil {
		t.Fatal("host_overide is a typo for host_override; loading it silently sends every request with the wrong SNI and Host")
	}
}
