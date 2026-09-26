package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractShowNamesAMissingProducerOnStderr(t *testing.T) {
	defer shopStatusWorkspace(t)()
	var err error
	stderr := captureStderr(t, func() {
		captureStdout(t, func() { err = contractShow([]string{"ConfirmOrder"}) })
	})
	if err != nil {
		t.Fatalf("contract show: %v", err)
	}
	if !strings.Contains(stderr, "id_order") || !strings.Contains(stderr, "CreateOrder") {
		t.Fatalf("the CHAIN STEP leaves id_order unwired because its producer is not in it; say so like chain new does:\n%q", stderr)
	}
}

func TestInitIgnoresPendingSafeSpotProposals(t *testing.T) {
	dir := shopWorkspace(t, "")
	restore := chdir(t, dir)
	defer restore()
	captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	body := readGitignore(t, dir)
	if !strings.Contains(body, ".shrt/safespots/pending/\n") {
		t.Fatalf("proposals are per-machine review material and must be gitignored:\n%s", body)
	}
	if strings.Contains(body, ".shrt/safespots/\n") {
		t.Fatalf("approved safe spots are committed; only pending/ is ignored:\n%s", body)
	}
}

func TestInitExampleChainAssertsTheConfiguredOKAndData(t *testing.T) {
	dir := shopWorkspace(t, shopConfig+"conventions:\n    envelope_path: status.code\n    envelope_ok: SUCCESS\n")
	restore := chdir(t, dir)
	defer restore()
	captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	raw, err := os.ReadFile(filepath.Join(dir, ".shrt", "chains", "example.yaml.template"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"equals: SUCCESS", "equals: ${vars.thing_name}", "REPLACE_ME"} {
		if !strings.Contains(text, want) {
			t.Errorf("the example must carry %q:\n%s", want, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Errorf("the example must carry no comment lines, a no-comment hook rejects them: %q", line)
		}
	}
}
