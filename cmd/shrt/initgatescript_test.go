package main

import (
	"os"
	"strings"
	"testing"
)

func TestInitWritesTheCIGateScriptAsAFile(t *testing.T) {
	dir := t.TempDir()
	defer chdir(t, dir)()
	out := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Errorf("init: %v", err)
		}
	})
	raw, err := os.ReadFile(".shrt/ci-gate.sh")
	if err != nil {
		t.Fatalf("init must write the CI gate as a file: %v\n%s", err, out)
	}
	if !strings.Contains(out, "write .shrt/ci-gate.sh") || !strings.Contains(string(raw), "shrt chain lint -strict\n") ||
		!strings.HasSuffix(string(raw), "exit 3; fi\n") {
		t.Fatalf("the gate file is the whole README block, and init says it wrote it:\n%s\n---\n%s", out, raw)
	}
}
