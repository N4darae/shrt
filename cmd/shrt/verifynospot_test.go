package main

import (
	"context"
	"strings"
	"testing"
)

func TestVerifyWithoutASafeSpotSaysSoPlainly(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	err := runVerify(context.Background(), []string{"cli-thing-flow"})
	if err == nil {
		t.Fatal("verify without a safe spot must refuse")
	}
	msg := err.Error()
	if strings.Contains(msg, "file does not exist") || !strings.HasPrefix(msg, "chain cli-thing-flow has no safe spot:") {
		t.Fatalf("say plainly there is no safe spot, without Go's error prefix: %q", msg)
	}
	if !strings.Contains(msg, ".shrt/safespots/cli-thing-flow.json") {
		t.Fatalf("name where it looked: %q", msg)
	}
}
