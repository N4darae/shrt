package main

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func approvedThingFlow(t *testing.T) *httptest.Server {
	t.Helper()
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	captureStdout(t, func() {
		if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-note", "create echoes the name"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	return srv
}

func TestCLIVerifyRefusesAVolatileMaskWiderThanTheApprovedOne(t *testing.T) {
	approvedThingFlow(t)
	path := ".shrt/chains/cli-thing-flow.yaml"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, strings.Replace(string(raw), "name: cli-thing-flow\n", "name: cli-thing-flow\nvolatile:\n    - '**'\n", 1))
	var verr error
	out := captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"})
	})
	if verr == nil || !strings.Contains(verr.Error(), "did not approve") {
		t.Fatalf("a mask the safe spot did not approve must fail verify, got %v:\n%s", verr, out)
	}
	if !strings.Contains(out, "did not approve: **") || !strings.Contains(out, "not counted: ") {
		t.Fatalf("verify must name the pattern and count what it hid:\n%s", out)
	}
}

func TestVerifyCountsTheValuesItMaskedOnOneLine(t *testing.T) {
	approvedThingFlow(t)
	for _, verbose := range []bool{false, true} {
		args := []string{"cli-thing-flow"}
		if verbose {
			args = append(args, "-v")
		}
		var err error
		out := captureStdout(t, func() { err = runVerify(context.Background(), args) })
		if err != nil || strings.Count(out, "not counted: ") != 1 || !strings.Contains(out, "(-masked lists them)") {
			t.Errorf("-v %v: the masked values are counted on one line: %v\n%s", verbose, err, out)
		}
	}
}
