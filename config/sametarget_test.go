package config_test

import (
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestTargetsThatDifferOnlyInSchemeOrHostCaseAreTheSame(t *testing.T) {
	same := [][2]string{
		{"HTTP://127.0.0.1:18121", "http://127.0.0.1:18121"},
		{"http://API.Example.test/", "http://api.example.test"},
		{" https://Example.test/v1/ ", "https://example.test/v1"},
	}
	for _, p := range same {
		if !config.SameTarget(p[0], p[1]) {
			t.Errorf("%q and %q name one target", p[0], p[1])
		}
	}
	differ := [][2]string{
		{"http://127.0.0.1:18121", "http://127.0.0.1:18130"},
		{"http://example.test", "https://example.test"},
		{"http://example.test/V1", "http://example.test/v1"},
	}
	for _, p := range differ {
		if config.SameTarget(p[0], p[1]) {
			t.Errorf("%q and %q are different targets", p[0], p[1])
		}
	}
}
