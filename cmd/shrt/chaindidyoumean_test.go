package main

import (
	"context"
	"strings"
	"testing"
)

func TestAMisspeltChainNameGetsADidYouMean(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	const typo = "cli-thing-flwo"
	for name, call := range map[string]func() error{
		"run":     func() error { return runRun(ctx, []string{typo}) },
		"verify":  func() error { return runVerify(ctx, []string{typo}) },
		"slice":   func() error { return chainSlice(ctx, []string{typo, "-step", "create"}) },
		"confirm": func() error { return runConfirm(ctx, []string{typo, "-note", "checked"}) },
		"diff":    func() error { return runDiff(ctx, []string{typo}) },
	} {
		var err error
		captureStdout(t, func() { err = call() })
		if err == nil || !strings.Contains(err.Error(), `did you mean "cli-thing-flow"?`) {
			t.Errorf("%s %s: want a did-you-mean naming cli-thing-flow, got %v", name, typo, err)
		}
	}
}
