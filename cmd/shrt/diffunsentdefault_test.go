package main

import (
	"context"
	"strings"
	"testing"
)

func TestDiffDoesNotShowAFieldDeclaredInOneRunAndNeverSent(t *testing.T) {
	total := 0
	addedFieldWorkspace(t, &total)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	var derr error
	out := captureStdout(t, func() { derr = runDiff(ctx, []string{"cli-thing-flow"}) })
	if derr != nil {
		t.Fatalf("a field one run's descriptor declares and the backend never sent is no difference, as verify reads it: %v\n%s", derr, out)
	}
	if !strings.Contains(out, "not on the wire") || !strings.Contains(out, "fetch total") {
		t.Fatalf("the field left out should be named:\n%s", out)
	}
	total = 7
	captureStdout(t, func() { _ = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	out = captureStdout(t, func() { derr = runDiff(ctx, []string{"cli-thing-flow"}) })
	if derr == nil || !strings.Contains(out, "total") {
		t.Fatalf("a value the backend now sends is still a difference:\n%s", out)
	}
}
