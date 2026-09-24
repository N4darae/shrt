package main

import (
	"context"
	"strings"
	"testing"
)

func TestAQuietCleanVerifyNamesTheChain(t *testing.T) {
	name, extra := "widget", false
	driftWorkspace(t, &name, &extra)
	var err error
	out := captureStdout(t, func() { err = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	if err != nil {
		t.Fatalf("verify: %v\n%s", err, out)
	}
	if !strings.Contains(out, "cli-thing-flow: no drift vs safe spot") {
		t.Fatalf("a gate log of many -quiet verifies must say which chain had no drift:\n%s", out)
	}
}
