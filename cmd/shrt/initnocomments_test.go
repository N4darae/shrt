package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestInitWritesAConfigWithNoCommentLines(t *testing.T) {
	dir := t.TempDir()
	restore := chdir(t, dir)
	defer restore()

	out := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})

	raw, err := os.ReadFile(filepath.Join(dir, ".shrt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Fatalf("line %d of the written config is a comment (%q). A repo whose pre-commit hook "+
				"blocks new comment lines cannot commit what init wrote without editing it by hand; "+
				"guidance belongs on stdout:\n%s", i+1, line, raw)
		}
	}
	for _, key := range []string{"envelope_path", "envelope_ok", "item_envelope_path", "read_only_prefixes", "code_fields", "validate_output"} {
		if !strings.Contains(out, key) {
			t.Errorf("init no longer writes the conventions guidance into the file, so it has to print "+
				"it; %s is missing from stdout:\n%s", key, out)
		}
	}
	if _, err := config.Load(dir); err != nil {
		t.Fatalf("the written config does not load: %v", err)
	}
}
