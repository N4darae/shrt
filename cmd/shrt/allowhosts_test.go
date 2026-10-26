package main

import (
	"context"
	"strings"
	"testing"
)

func TestRunAndGateRefuseAHostNotAllowedButDryRunStillResolves(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://api.example.test")
	err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"})
	if err == nil || !strings.Contains(err.Error(), "target.base_url names api.example.test") ||
		!strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("run: want a refusal before sending, got %v", err)
	}
	if code := exitCodeOf(err); code != 1 {
		t.Fatalf("run: want exit 1, got %d", code)
	}
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-dry-run", "-quiet"}); err != nil {
		t.Fatalf("run -dry-run sends nothing, so the host does not matter: %v", err)
	}
	if err := runGate(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "allow_hosts") {
		t.Fatalf("gate: want one refusal up front, got %v", err)
	}
}
