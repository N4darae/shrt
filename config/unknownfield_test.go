package config_test

import (
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestLoadRejectsUnknownTargetKey(t *testing.T) {
	root := write(t, "target:\n    base_url: http://x\n    host_overide: y\n")
	if _, err := config.Load(root); err == nil {
		t.Fatal("host_overide is a typo for host_override; loading it silently sends every request with the wrong SNI and Host")
	}
}
