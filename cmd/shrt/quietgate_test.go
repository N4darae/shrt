package main

import (
	"context"
	"strings"
	"testing"
)

func nonEmptyLines(out string) []string {
	lines := []string{}
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestAQuietGreenRunAndVerifyPrintOneLineEach(t *testing.T) {
	name, extra := "widget", false
	driftWorkspace(t, &name, &extra)
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if lines := nonEmptyLines(out); len(lines) != 1 || !strings.HasPrefix(lines[0], "cli-thing-flow: PASSED") {
		t.Fatalf("a green -quiet run prints its verdict line only, got:\n%s", out)
	}
	if strings.Contains(out, "\n\n") || strings.HasPrefix(out, "\n") {
		t.Fatalf("no blank lines under -quiet:\n%q", out)
	}
	out = captureStdout(t, func() { err = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	if err != nil {
		t.Fatalf("verify: %v\n%s", err, out)
	}
	if lines := nonEmptyLines(out); len(lines) != 1 || !strings.HasPrefix(lines[0], "cli-thing-flow: no drift vs safe spot") {
		t.Fatalf("a clean -quiet verify prints its verdict line only, got:\n%s", out)
	}
	if strings.HasPrefix(out, "\n") {
		t.Fatalf("no blank lines under -quiet:\n%q", out)
	}
}

func TestAQuietFailingRunStillSaysWhereItsRecordIs(t *testing.T) {
	name, extra := "widget", false
	driftWorkspace(t, &name, &extra)
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml")), "equals: widget", "equals: gizmo", 1))
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	if err == nil {
		t.Fatalf("the chain expects a name the backend does not send:\n%s", out)
	}
	if !strings.Contains(out, "FAILED") || !strings.Contains(out, "run ") || !strings.Contains(out, ".json") {
		t.Fatalf("a red -quiet run keeps its verdict, its failure and the record path:\n%s", out)
	}
}
