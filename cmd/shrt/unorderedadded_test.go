package main

import (
	"context"
	"strings"
	"testing"
)

func TestVerifyNotesAnUnorderedPathAddedAfterApproval(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	edited := strings.Replace(uniqueNameChain, "    - id: fetch\n", "    - id: fetch\n      unordered: [items]\n", 1)
	writeFile(t, ".shrt/chains/cli-unique.yaml", edited)
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=u1"}) })
	if err != nil {
		t.Fatalf("an unordered declaration can hide only a change of order, so it does not fail verify: %v\n%s", err, out)
	}
	if !strings.Contains(out, "unordered: [items]") || !strings.Contains(out, "fetch") || !strings.Contains(out, "only") {
		t.Fatalf("verify must note the unordered path added since approval and why it does not fail:\n%s", out)
	}
}

func TestConfirmSupersedeListsAnUnorderedPathAddedAfterApproval(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	edited := strings.Replace(uniqueNameChain, "    - id: fetch\n", "    - id: fetch\n      unordered: [items]\n", 1)
	writeFile(t, ".shrt/chains/cli-unique.yaml", edited)
	var err error
	out := captureStdout(t, func() {
		if err = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=u2"}); err != nil {
			return
		}
		err = runConfirm(ctx, []string{"cli-unique", "-supersede", "-note", "items are a set"})
	})
	if err != nil {
		t.Fatalf("propose: %v\n%s", err, out)
	}
	pending := string(mustRead(t, ".shrt/safespots/pending/cli-unique.md"))
	for name, text := range map[string]string{"summary": out, "pending report": pending} {
		if !strings.Contains(text, "unordered: [items]") {
			t.Errorf("the %s must list the unordered path added since the replaced safe spot:\n%s", name, text)
		}
	}
}
