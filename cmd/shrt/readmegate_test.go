package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
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
said="FAKE_SAY_$(echo "$1" | tr a-z A-Z)"
[ -n "${!said:-}" ] && printf '%s\n' "${!said}"
exit "${code:-0}"
`

func runReadmeGateWith(t *testing.T, env ...string) (string, int, string) {
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
	if !slices.Contains(env, "NO_OVERLAY=1") {
		writeFile(t, filepath.Join(work, ".shrt", "contracts", "items.yaml"), "domain: items\n")
	}
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
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_DIR="+dir)
	cmd.Env = append(cmd.Env, env...)
	out, _ := cmd.CombinedOutput()
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	return string(out), cmd.ProcessState.ExitCode(), string(calls)
}

func TestTheReadmeGateRunsTheStaticChecksThenShrtGate(t *testing.T) {
	out, code, calls := runReadmeGateWith(t)
	if code != 0 {
		t.Fatalf("a green wrapper exits 0, got %d:\n%s\n%s", code, out, calls)
	}
	want := "catalog build\ndoctor -strict\ncontract lint\ncontract quality -gate -baseline .shrt/quality-baseline\nchain lint -strict\ngate\n"
	if calls != want {
		t.Fatalf("the wrapper runs the static checks in order, then shrt gate:\n%s", calls)
	}
}

func TestTheReadmeGateExitsAsShrtGateDoes(t *testing.T) {
	for _, code := range []int{1, 3} {
		_, got, _ := runReadmeGateWith(t, "FAKE_GATE="+strconv.Itoa(code))
		if got != code {
			t.Fatalf("shrt gate exiting %d must exit the wrapper %d, got %d", code, code, got)
		}
	}
	_, got, calls := runReadmeGateWith(t, "FAKE_DOCTOR=1")
	if got != 1 || strings.Contains(calls, "gate") {
		t.Fatalf("a failed static check stops the wrapper before the gate: exit %d\n%s", got, calls)
	}
}

func TestTheReadmeGateSkipsTheContractChecksWithoutAnOverlay(t *testing.T) {
	out, code, calls := runReadmeGateWith(t, "NO_OVERLAY=1", "FAKE_CONTRACT=1")
	if code != 0 || strings.Contains(calls, "contract") || !strings.Contains(calls, "chain lint -strict\ngate\n") {
		t.Fatalf("a repo with chains and no contracts passes the wrapper, which never runs the contract checks: exit %d\n%s\n%s", code, out, calls)
	}
	_, code, _ = runReadmeGateWith(t, "FAKE_CONTRACT=1")
	if code != 1 {
		t.Fatalf("with an overlay, a contract error still fails the wrapper, got %d", code)
	}
}
