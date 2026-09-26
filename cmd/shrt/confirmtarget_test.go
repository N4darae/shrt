package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLIConfirmComparesOnlyWithAnEarlierRunOfTheSameTarget(t *testing.T) {
	first := newEchoNameBackend()
	t.Cleanup(first.Close)
	chdirToFreshCLIWorkspace(t, first.URL)
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	second := newEchoNameBackend()
	t.Cleanup(second.Close)
	cfg := string(mustRead(t, ".shrt/config.yaml"))
	writeFile(t, ".shrt/config.yaml", strings.Replace(cfg, first.URL, second.URL, 1))
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	var err error
	out := captureStdout(t, func() {
		err = runConfirm(context.Background(), []string{"cli-thing-flow", "-note", "create echoes the name"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Compared with the earlier passing run") {
		t.Fatalf("the only earlier run hit another target, so it says nothing about fields that change every run here:\n%s", out)
	}
	if !strings.Contains(out, "against `"+second.URL+"`") {
		t.Fatalf("the summary must say no earlier run against this target exists:\n%s", out)
	}
}
