package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestChainHollowExplainsARenamedOrphanAsARename(t *testing.T) {
	renameThingFlow(t, nil)
	if out, err := confirmRename(t, "-by", "bob@example.test"); err != nil {
		t.Fatalf("rename: %v\n%s", err, out)
	}
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{"cli-renamed", "-quiet"}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	out := captureStdout(t, func() { _ = chainHollow(nil) })
	if !strings.Contains(out, "orphan  cli-thing-flow  (renamed to cli-renamed") {
		t.Fatalf("the orphan must name its new chain:\n%s", out)
	}
	if strings.Contains(out, "Deleting a chain leaves its runs behind") {
		t.Fatalf("a renamed chain was not deleted, so the deletion wording does not apply:\n%s", out)
	}
	if !strings.Contains(out, "Renaming a chain leaves its runs under the old name") {
		t.Fatalf("a renamed orphan needs rename wording:\n%s", out)
	}

	writeFile(t, ".shrt/runs/gone/20260101T000000Z-deadbeef.json", "{}")
	defer os.RemoveAll(".shrt/runs/gone")
	out = captureStdout(t, func() { _ = chainHollow(nil) })
	if !strings.Contains(out, "Deleting a chain leaves its runs behind") || !strings.Contains(out, "Renaming a chain") {
		t.Fatalf("a deleted and a renamed orphan each get their wording:\n%s", out)
	}
}
