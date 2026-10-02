package main

import (
	"os"
	"strings"
	"testing"
)

func TestInitWritesAZeroQualityBaselineOnlyWhereThereIsNone(t *testing.T) {
	dir := t.TempDir()
	defer chdir(t, dir)()
	out := cliInit(t)
	if raw, err := os.ReadFile(".shrt/quality-baseline"); err != nil || string(raw) != "0\n" {
		t.Fatalf("init writes 0 into the baseline the gate script reads: %v %q", err, raw)
	}
	if gate, _ := os.ReadFile(".shrt/ci-gate.sh"); !strings.Contains(string(gate), "-baseline .shrt/quality-baseline\n") {
		t.Errorf("the gate script reads the baseline init writes:\n%s", gate)
	}
	if !strings.Contains(out, "write .shrt/ci-gate.sh, .shrt/quality-baseline (0)\n") {
		t.Errorf("one written-files line names both:\n%s", out)
	}
	writeFile(t, ".shrt/quality-baseline", "7\n")
	again := cliInit(t, "-force")
	if raw, _ := os.ReadFile(".shrt/quality-baseline"); string(raw) != "7\n" || strings.Contains(again, "quality-baseline") {
		t.Fatalf("a baseline that exists is the reviewed score, never overwritten, -force or not: %q\n%s", raw, again)
	}
	os.Remove(".shrt/quality-baseline")
	if again = cliInit(t); !strings.Contains(again, "write .shrt/quality-baseline (0)\n") {
		t.Errorf("a missing baseline is written beside a kept gate script:\n%s", again)
	}
}
