package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestInitListsTheContractsDirAndARerunClaimsNoWrites(t *testing.T) {
	dir := t.TempDir()
	restore := chdir(t, dir)
	defer restore()

	first := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Errorf("init: %v", err)
		}
	})
	if !strings.Contains(first, ".shrt/contracts/") {
		t.Fatalf("init creates .shrt/contracts/ and must say so:\n%s", first)
	}
	second := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Errorf("init again: %v", err)
		}
	})
	if strings.Contains(second, "write .shrt/{chains") || strings.Contains(second, "write .shrt/chains") {
		t.Fatalf("a rerun created no directory, so it must not print a write for one:\n%s", second)
	}
	if strings.Contains(second, "shrt contract init -all") {
		t.Fatalf("a rerun must not repeat the first-time next: list:\n%s", second)
	}
	if !strings.Contains(second, "shrt doctor") {
		t.Fatalf("a rerun still points at doctor:\n%s", second)
	}
}

func TestContractInitDoesNotAskToFillTODOsThereAreNone(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	var err error
	out := captureStdout(t, func() { err = runContract(context.Background(), []string{"init", "-all"}) })
	if err != nil {
		t.Fatalf("contract init: %v", err)
	}
	if !strings.Contains(out, "TODO") {
		t.Fatalf("a fresh scaffold is full of TODOs and must say so:\n%s", out)
	}
	files, _ := filepath.Glob(filepath.Join(".shrt", "contracts", "*.yaml"))
	if len(files) == 0 {
		t.Fatal("contract init -all wrote nothing")
	}
	todo := regexp.MustCompile(`'?TODO[^'\n]*'?`)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, todo.ReplaceAll(raw, []byte("checked")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out = captureStdout(t, func() { err = runContract(context.Background(), []string{"init", "-all"}) })
	if err != nil {
		t.Fatalf("contract init again: %v", err)
	}
	left := 0
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		left += strings.Count(string(raw), "TODO")
	}
	if left == 0 && strings.Contains(out, "fill every TODO") {
		t.Fatalf("no TODO is left in what was written, so asking to fill every TODO sends the reader hunting:\n%s", out)
	}
	if left > 0 && !strings.Contains(out, strconv.Itoa(left)+" TODO") {
		t.Fatalf("%d TODO(s) remain and the output must count them:\n%s", left, out)
	}
}
