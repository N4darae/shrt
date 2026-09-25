package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestInitIgnoresTheScratchDirTheREADMERecommends(t *testing.T) {
	root := t.TempDir()
	if _, err := ensureGitignore(root, initGitignore(config.Default())); err != nil {
		t.Fatal(err)
	}
	if body := readGitignore(t, root); !strings.Contains(body, ".shrt/scratch/\n") {
		t.Fatalf("the README writes exploratory slices to .shrt/scratch/, so init ignores it: %q", body)
	}
}
