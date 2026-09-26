package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIVerifyAgainstAStoppedBackendIsNotARegression(t *testing.T) {
	srv := newFakeCLIBackend()
	chdirToFreshCLIWorkspace(t, srv.URL)
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	entries, err := os.ReadDir(".shrt/runs/cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(".shrt/runs/cli-thing-flow", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	spotRaw, _ := json.Marshal(map[string]any{
		"chain": "cli-thing-flow", "run_id": rec["run_id"], "target": rec["target"],
		"confirmed_by": "test@example.test", "confirmed_at": "2026-01-01T00:00:00Z", "digest": "d", "steps": rec["steps"],
	})
	writeFile(t, ".shrt/safespots/cli-thing-flow.json", string(spotRaw))
	resealSafeSpot(t, ".shrt/safespots/cli-thing-flow.json")
	srv.Close()

	var verr error
	out := captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet", "-save=false"})
	})
	if verr == nil || strings.Contains(verr.Error(), "regression") {
		t.Fatalf("a replay that never reached the backend is not a regression, got %v\n%s", verr, out)
	}
	if exitCodeOf(verr) != 3 || !strings.Contains(verr.Error(), "could not verify") {
		t.Fatalf("an unreachable target must say it could not verify and exit 3 like an errored run, got %d %v", exitCodeOf(verr), verr)
	}
	if strings.Contains(out, "change(s) vs safe spot") || strings.Contains(out, "want=passed") {
		t.Fatalf("nothing was answered, so there is no change to count and no step status to compare; print only why it could not verify:\n%s", out)
	}
}
