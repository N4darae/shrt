package main

import (
	"context"
	"strings"
	"testing"
)

func TestSliceNextKeepsTheChainPathItWasGiven(t *testing.T) {
	shop := newFakeShop()
	shop.cancelConfirmedBug = true
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/scratch/probe-orders.yaml", cancelConfirmedChain)
	_ = runRun(context.Background(), []string{".shrt/scratch/probe-orders.yaml", "-quiet", "-var", "tag=src"})

	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{".shrt/scratch/probe-orders.yaml", "-step", "cancel_confirmed",
			"-run", "latest", "-verify", "-var", "tag=s1"})
	})
	next := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "next: ") {
			next = strings.TrimSpace(l)
		}
	}
	if next == "" {
		t.Fatalf("the slice dropped the confirm the target needs, so a next: command is expected (err %v):\n%s", err, out)
	}
	if !strings.Contains(next, "shrt chain slice .shrt/scratch/probe-orders.yaml ") {
		t.Fatalf("the chain was given by path and is not under paths.chains, so next: must name that path, not the chain name:\n%s", next)
	}
}
