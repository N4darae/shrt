package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestTheReadmeGateCheckLeaksNoVariableButItsVerdicts(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	script := readmeGateScript(t)
	start := strings.Index(script, "check() {")
	if start < 0 {
		t.Fatal("the README gate has no check() function")
	}
	end := strings.Index(script[start:], "\n}\n")
	if end < 0 {
		t.Fatal("the README gate's check() is not closed")
	}
	fn := script[start : start+end+3]
	probe := "shrt() { return 0; }\nsleep() { :; }\n" + fn +
		"tag=t; fail=0; unverified=0\n" +
		"before=$(compgen -v | sort)\n" +
		"check run flow /dev/null \"\"\n" +
		"comm -13 <(echo \"$before\") <(compgen -v | sort) | grep -vx 'before' || true\n"
	out, err := exec.Command("bash", "-c", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	if leaked := strings.TrimSpace(string(out)); leaked != "" {
		t.Fatalf("check() sets globals it should declare local: %s", leaked)
	}
}
