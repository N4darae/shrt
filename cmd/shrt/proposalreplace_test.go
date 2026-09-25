package main

import (
	"context"
	"strings"
	"testing"
)

func TestASecondProposalSaysWhichPendingProposalItReplaces(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	propose := func() string {
		return captureStdout(t, func() {
			if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
				t.Fatalf("shrt run: %v", err)
			}
			if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "fetch returns the created name"}); err != nil {
				t.Fatalf("propose: %v", err)
			}
		})
	}
	first := propose()
	if strings.Contains(first, "replaces pending proposal") {
		t.Fatalf("nothing was pending yet:\n%s", first)
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.store.LoadProposal("cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	second := propose()
	if !strings.Contains(second, "replaces pending proposal "+p.RunID) {
		t.Fatalf("a second proposal silently replaced the pending one of run %s:\n%s", p.RunID, second)
	}
}
