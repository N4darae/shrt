package main

import (
	"context"
	"strings"
	"testing"
)

func TestUnusedVarErrorSaysSoWhenTheChainReadsNoVars(t *testing.T) {
	msg := unusedVarError([]string{"tag"}, "probe", nil).Error()
	if strings.Contains(msg, "vars this chain reads: \n") || strings.HasSuffix(msg, "vars this chain reads: ") {
		t.Fatalf("an empty list reads as a truncated message: %q", msg)
	}
	if !strings.Contains(msg, "reads no vars") || !strings.Contains(msg, "-var tag") {
		t.Fatalf("want the error kept and the empty list worded, got %q", msg)
	}
}

func TestUnusedVarErrorListsTheVarsTheChainReads(t *testing.T) {
	msg := unusedVarError([]string{"tagg"}, "probe", []string{"run_tag", "sku"}).Error()
	if !strings.Contains(msg, "vars this chain reads: run_tag, sku") {
		t.Fatalf("got %q", msg)
	}
}

func TestAnUnreadVarIsAWarningAndATypoOfARealOneIsRefused(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	writeFile(t, ".shrt/chains/cli-plain.yaml", strings.NewReplacer("widget ${vars.tag}", "plain widget", "vars:\n    tag: first\n", "", "name: cli-unique", "name: cli-plain").Replace(uniqueNameChain))
	ctx := context.Background()
	var err error
	stderr := captureStderr(t, func() {
		captureStdout(t, func() { err = runRun(ctx, []string{"cli-plain", "-quiet", "-var", "tag=loop1"}) })
	})
	if err != nil {
		t.Fatalf("a loop passing -var tag to every chain must not stop at a chain that reads none: %v", err)
	}
	if stderr != "" {
		t.Fatalf("a chain that reads no vars at all takes any -var silently, got %q", stderr)
	}
	stderr = captureStderr(t, func() {
		captureStdout(t, func() { err = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "zzz=loop1"}) })
	})
	if err != nil || !strings.Contains(stderr, `warning: -var zzz: chain "cli-unique" never reads it (it reads tag)`) {
		t.Fatalf("an unread var of a chain that reads others is named in a warning: %v %q", err, stderr)
	}
	captureStdout(t, func() { err = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tga=loop1"}) })
	if err == nil || !strings.Contains(err.Error(), "looks mistyped") || !strings.Contains(err.Error(), "(tag)") {
		t.Fatalf("a name one edit from a var the chain reads is still refused: %v", err)
	}
}
