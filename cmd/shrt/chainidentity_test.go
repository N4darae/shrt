package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestAChainWhoseNameDiffersFromItsFileIsRefusedEverywhere(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	raw, err := os.ReadFile(".shrt/chains/cli-thing-flow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(string(raw), "name: cli-thing-flow\n", "name: thing-new\n", 1))

	for _, args := range [][]string{{"verify", "cli-thing-flow"}, {"verify", "thing-new"}, {"run", "cli-thing-flow"}} {
		var got error
		out := captureStdout(t, func() {
			if args[0] == "run" {
				got = runRun(ctx, append(args[1:], "-quiet"))
			} else {
				got = runVerify(ctx, append(args[1:], "-quiet"))
			}
		})
		if got == nil || !strings.Contains(got.Error(), "declares name: thing-new") || !strings.Contains(got.Error(), "rename the file to thing-new.yaml") {
			t.Errorf("shrt %s: the file and its name disagree on which safe spot it has, so it is refused: %v\n%s", strings.Join(args, " "), got, out)
		}
	}
}
