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
