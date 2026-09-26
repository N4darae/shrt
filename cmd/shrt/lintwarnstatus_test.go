package main

import (
	"strings"
	"testing"
)

func TestChainLintMarksAChainWithOnlyWarningsWarn(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	appendAuth(t, "${env.SHRT_TEST_UNSET_WIDGET_USER}")
	var err error
	out := captureStdout(t, func() { err = chainLint(nil) })
	if err != nil {
		t.Fatalf("warnings alone do not fail lint: %v\n%s", err, out)
	}
	found := false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, " cli-thing-flow") {
			found = true
			if !strings.HasPrefix(line, "warn ") {
				t.Fatalf("a chain with only warnings needs a status, got %q:\n%s", line, out)
			}
		}
	}
	if !found || !strings.Contains(out, "WARN") {
		t.Fatalf("want a warning under the chain's line:\n%s", out)
	}
}
