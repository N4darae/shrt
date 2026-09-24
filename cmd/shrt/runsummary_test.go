package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestRunSummaryNamesEveryStepAKeepGoingRunLeftRed(t *testing.T) {
	rec := &runner.Record{
		Chain:       "keep-going",
		Status:      runner.StatusFailed,
		KeepGoing:   true,
		FailedSteps: []string{"create", "fetch", "list"},
		Failure:     "-keep-going: 3 of 5 steps did not pass",
	}
	out := summary(rec, false)
	if !strings.Contains(out, "did not pass: create, fetch, list") {
		t.Fatalf("the summary must list every step that did not pass, got:\n%s", out)
	}
}

func TestCLIRunStampsTheBuildLabelIntoTheSavedRecord(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)

	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet", "-build", "rc-2"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	entries, err := os.ReadDir(".shrt/runs/cli-thing-flow")
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one saved run record: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(".shrt/runs/cli-thing-flow", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec["build"] != "rc-2" {
		t.Fatalf("saved record build = %v, want rc-2", rec["build"])
	}
}

func TestRunSummaryNamesTheBuild(t *testing.T) {
	rec := &runner.Record{Chain: "c", Status: runner.StatusPassed, Target: "http://x", Build: "rc-2"}
	if out := summary(rec, false); !strings.Contains(out, "rc-2") {
		t.Fatalf("the summary must name the build the run was stamped with, got:\n%s", out)
	}
}
