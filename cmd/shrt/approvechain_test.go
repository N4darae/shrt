package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestApproveRefusesWhenTheChainChangedSinceTheProposal(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	path := ".shrt/chains/cli-thing-flow.yaml"
	original := string(mustRead(t, path))
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "fetch returns the created name"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
	})
	edited := strings.Replace(original, "kind: KIND_A", "kind: KIND_B", 1)
	if edited == original {
		t.Fatal("fixture chain has no kind: KIND_A to edit")
	}
	writeFile(t, path, edited)
	var err error
	captureStdout(t, func() {
		err = runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"})
	})
	if err == nil {
		t.Fatal("approve wrote a safe spot for a chain that is not the one the proposal was run from")
	}
	for _, want := range []string{"refusing to approve cli-thing-flow", "step create: body kind", "propose that run"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal lacks %q:\n%v", want, err)
		}
	}
	if _, serr := os.Stat(".shrt/safespots/cli-thing-flow.json"); serr == nil {
		t.Fatal("a refused approval must not write the safe spot")
	}
	writeFile(t, path, original)
	captureStdout(t, func() {
		err = runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"})
	})
	if err != nil {
		t.Fatalf("the chain is back as proposed, approve must succeed: %v", err)
	}
}
