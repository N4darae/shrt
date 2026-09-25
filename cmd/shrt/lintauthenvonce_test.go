package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChainLintNamesUnsetLoginVariablesOnceForTheWholeLint(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	appendAuth(t, "${env.WIDGET_USER}")
	for _, name := range []string{"WIDGET_USER", "WIDGET_PASSWORD"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(".shrt", "chains", "cli-thing-flow.yaml")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	second := strings.Replace(string(raw), "name: cli-thing-flow", "name: cli-thing-again", 1)
	writeFile(t, filepath.Join(".shrt", "chains", "cli-thing-again.yaml"), second)

	out := captureStdout(t, func() { _ = chainLint(nil) })
	if n := strings.Count(out, "not exported in this shell"); n != 1 {
		t.Fatalf("two chains share one login whose variables are unset; lint says so once, not %d times:\n%s", n, out)
	}
	if !strings.Contains(out, "WIDGET_PASSWORD, WIDGET_USER") || !strings.Contains(out, "the 2 chain(s)") {
		t.Fatalf("the one line names the variables and how many chains run cannot start:\n%s", out)
	}
}
