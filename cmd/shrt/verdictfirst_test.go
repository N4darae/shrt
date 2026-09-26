package main

import (
	"context"
	"strings"
	"testing"
)

func TestANonBackendVerdictLeadsAndSkipsTheDriftDump(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	createForeignThing(t, srv.URL, "widget lead1")
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=lead1"}) })
	if err == nil || !strings.Contains(err.Error(), "fixture collision") {
		t.Fatalf("want a fixture collision: %v\n%s", err, out)
	}
	lines := []string{}
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "could not verify cli-unique: fixture collision") {
		t.Fatalf("the verdict must be the first line:\n%s", out)
	}
	if strings.Contains(out, "[create] changed") || strings.Contains(out, "map[") {
		t.Fatalf("a non-backend verdict prints no drift dump:\n%s", out)
	}
	if !strings.Contains(out, "create (failed)") {
		t.Fatalf("the affected steps are listed briefly:\n%s", out)
	}
}
