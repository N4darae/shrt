package agentkit

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateScriptIsTheWholeReadmeBlock(t *testing.T) {
	raw, err := GateScript()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte("#!/usr/bin/env bash\nset -euo pipefail\n")) {
		t.Fatalf("the script starts with its shebang and the block's first line:\n%s", raw[:80])
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if last := lines[len(lines)-1]; last != "exec shrt gate" {
		t.Fatalf("the script must end with the block's last line, got %q", last)
	}
	if strings.Contains(string(raw), "```") {
		t.Fatal("no fence may leak into the script")
	}
	if _, err := exec.LookPath("bash"); err == nil {
		cmd := exec.Command("bash", "-n")
		cmd.Stdin = bytes.NewReader(raw)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bash -n rejects the gate script: %v\n%s", err, out)
		}
	}
}

func TestInstallGateScriptKeepsAnEditedCopyUnlessForced(t *testing.T) {
	root := t.TempDir()
	if wrote, err := InstallGateScript(root, false); err != nil || !wrote {
		t.Fatalf("first install writes it: %v %v", wrote, err)
	}
	path := filepath.Join(root, filepath.FromSlash(GateScriptPath))
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("the script is executable: %v %v", info, err)
	}
	if err := os.WriteFile(path, []byte("edited\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if wrote, err := InstallGateScript(root, false); err != nil || wrote {
		t.Fatalf("a re-run keeps the copy: %v %v", wrote, err)
	}
	if wrote, err := InstallGateScript(root, true); err != nil || !wrote {
		t.Fatalf("-force rewrites it: %v %v", wrote, err)
	}
	if got, _ := os.ReadFile(path); string(got) == "edited\n" {
		t.Fatal("-force must restore the script")
	}
}
