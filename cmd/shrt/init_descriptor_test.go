package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIInitFailsLoudlyWhenTheDescriptorCannotBeBuilt(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	empty := t.TempDir()
	t.Setenv("PATH", empty)

	err = runInit(context.Background(), []string{"-agents=false"})
	if err == nil {
		t.Fatal("shrt init reported success with no descriptor built: every catalog, contract and chain " +
			"command then dead-ends, and a script or CI reading the exit code sees adoption succeed")
	}
	var coded *exitError
	if !errors.As(err, &coded) {
		t.Fatalf("init failed with %v, which exits 1 — misuse and unusable state should exit 2", err)
	}
	if coded.code != 2 {
		t.Errorf("exit code = %d, want 2", coded.code)
	}
	if !strings.Contains(err.Error(), "shrt catalog build") {
		t.Errorf("the failure does not name the command that fixes it: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(dir, ".shrt", "config.yaml")); statErr != nil {
		t.Errorf("init should still have written the config it got as far as: %v", statErr)
	}
}

func TestBuildingTheDescriptorNamesTheMissingToolRatherThanTheConfig(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	if err := runInit(context.Background(), []string{"-build=false", "-agents=false"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	err = runCatalog(context.Background(), []string{"build"})
	if err == nil {
		t.Fatal("catalog build succeeded with no buf on PATH")
	}
	msg := err.Error()
	if !strings.Contains(msg, "not on PATH") {
		t.Errorf("the error does not say the tool is missing, so a reader retunes descriptor.source instead: %v", err)
	}
	if !strings.Contains(msg, "buf") {
		t.Errorf("the error does not name buf, which is the undocumented prerequisite: %v", err)
	}
}
