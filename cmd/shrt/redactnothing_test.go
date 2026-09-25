package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestAnUnapprovedRedactThatBlankedNothingSaysSo(t *testing.T) {
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	raw, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", string(raw)+"redact:\n    - '**.nickname'\n")
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if err == nil {
		t.Fatalf("an unapproved redact pattern still fails verify:\n%s", out)
	}
	if strings.Contains(err.Error(), "the value(s) they blanked were not compared") || !strings.Contains(err.Error(), "hid nothing this run") {
		t.Fatalf("the pattern blanked nothing, so verify must not say values it blanked were not compared: %v", err)
	}
	if !strings.Contains(out, "hid nothing this run") {
		t.Fatalf("the report line says it hid nothing this run too:\n%s", out)
	}
}
