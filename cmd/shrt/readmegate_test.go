package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func readmeGateScript(t *testing.T) string {
	t.Helper()
	readme := string(mustRead(t, "../../README.md"))
	_, section, ok := strings.Cut(readme, "### CI gate")
	if !ok {
		t.Fatal("README has no CI gate section")
	}
	_, block, ok := strings.Cut(section, "```bash\n")
	if !ok {
		t.Fatal("the CI gate section has no bash block")
	}
	script, _, ok := strings.Cut(block, "```")
	if !ok {
		t.Fatal("the CI gate bash block is not closed")
	}
	return script
}

const fakeShrt = `#!/usr/bin/env bash
n=0
[ -f "$FAKE_DIR/count-$1" ] && n=$(cat "$FAKE_DIR/count-$1")
n=$((n+1))
echo "$n" > "$FAKE_DIR/count-$1"
echo "$*" >> "$FAKE_DIR/calls"
codes="FAKE_$(echo "$1" | tr a-z A-Z)"
code=$(echo "${!codes:-0}" | cut -d, -f"$n")
exit "${code:-0}"
`

func runReadmeGate(t *testing.T, verifyCodes string) (string, int, string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	work := filepath.Join(dir, "work")
	for _, d := range []string{bin, filepath.Join(work, ".shrt", "chains"), filepath.Join(work, ".shrt", "safespots"), filepath.Join(work, ".shrt", "docs")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(work, ".shrt", "docs", "GRAMMAR.md"), "grammar\n")
	writeFile(t, filepath.Join(work, ".shrt", "chains", "flow.yaml"), "name: flow\nvars:\n  tag: t\n")
	writeFile(t, filepath.Join(work, ".shrt", "safespots", "flow.json"), "{}\n")
	writeFile(t, filepath.Join(work, "gate.sh"), readmeGateScript(t))
	for name, body := range map[string]string{
		"shrt":  fakeShrt,
		"git":   "#!/usr/bin/env bash\npwd\n",
		"sleep": "#!/usr/bin/env bash\necho \"$*\" >> \"$FAKE_DIR/slept\"\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", "gate.sh")
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_DIR="+dir, "FAKE_VERIFY="+verifyCodes)
	out, _ := cmd.CombinedOutput()
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	return string(out), cmd.ProcessState.ExitCode(), string(calls)
}

func TestTheReadmeGateRetriesAVerifyThatCouldNotVerify(t *testing.T) {
	out, code, calls := runReadmeGate(t, "3,0")
	if code != 0 {
		t.Fatalf("a verify that exits 3 and then 0 on the retry passes the gate, got exit %d:\n%s\n%s", code, out, calls)
	}
	if strings.Count(calls, "verify flow") != 2 {
		t.Fatalf("the verify is retried once:\n%s", calls)
	}
}

func TestTheReadmeGateSaysCouldNotVerifyWhenTheRetryExits3Too(t *testing.T) {
	out, code, calls := runReadmeGate(t, "3,3")
	if code == 0 {
		t.Fatalf("two exit 3 in a row fail the gate:\n%s", out)
	}
	if strings.Count(calls, "verify flow") != 2 {
		t.Fatalf("the verify is retried once, not more:\n%s", calls)
	}
	if !strings.Contains(out, "could not verify") || strings.Contains(out, "gate: verify flow exited 3") {
		t.Fatalf("an exit 3 twice is reported as could not verify, not as a failed verify:\n%s", out)
	}
}

func TestTheReadmeGateDoesNotRetryARegression(t *testing.T) {
	out, code, calls := runReadmeGate(t, "1")
	if code != 1 || strings.Count(calls, "verify flow") != 1 || !strings.Contains(out, "gate: verify flow exited 1") {
		t.Fatalf("a regression fails the gate at once, exit 1, got %d:\n%s\n%s", code, out, calls)
	}
}
