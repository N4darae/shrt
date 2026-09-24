package main

import (
	"context"
	"testing"
)

func TestSupersedeDoesNotListFixtureEchoesAsDifferences(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=second"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-unique", "-supersede", "-note", "same backend, fresh tag"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
	})
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.store.LoadProposal("cli-unique")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Replaced) != 0 {
		t.Fatalf("a request and response differing only in the fixture name is what verify masks, not a difference to sign off: %+v", p.Replaced)
	}
}
