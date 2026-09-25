package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitRerunRewritesTheUntouchedExampleWithTheObservedEnvelope(t *testing.T) {
	dir := shopWorkspace(t, "")
	restore := chdir(t, dir)
	defer restore()
	initOnce := func() string {
		return captureStdout(t, func() {
			if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
				t.Fatalf("init: %v", err)
			}
		})
	}
	initOnce()
	example := filepath.Join(dir, ".shrt", "chains", "example.yaml.template")
	cfgPath := filepath.Join(dir, ".shrt", "config.yaml")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, cfgPath, string(cfg)+"conventions:\n    envelope_path: status.code\n    envelope_ok: SUCCESS\n")
	out := initOnce()
	raw, err := os.ReadFile(example)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), exampleOKPlaceholder) || strings.Count(string(raw), "- path: status.code\n        equals: SUCCESS") != 2 {
		t.Fatalf("a re-run that finds envelope_ok must rewrite the untouched scaffold:\n%s", raw)
	}
	if !strings.Contains(out, "write .shrt/chains/example.yaml.template") {
		t.Fatalf("init must say it rewrote the template:\n%s", out)
	}

	edited := strings.Replace(string(raw), "SUCCESS", exampleOKPlaceholder, 1) + "# mine\n"
	writeFile(t, example, edited)
	initOnce()
	if got, _ := os.ReadFile(example); string(got) != edited {
		t.Fatalf("a template the user edited must be kept:\n%s", got)
	}
}
