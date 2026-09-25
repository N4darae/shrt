package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLISliceVerifyPrintsTheHeaderThenOneLinePerRepeat(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeNoisyChain(t, "widget")
	if err := runRun(context.Background(), []string{"cli-noisy-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	out, _ := sliceVerify(t)
	header := strings.Index(out, "slice of cli-noisy-flow for step fetch")
	progress := strings.Index(out, "repeat 1 of 3: ")
	if header < 0 || progress < 0 || strings.Count(out, "\nrepeat ") != 3 || strings.Contains(out, "ok     1 create") {
		t.Fatalf("want the header and one line per repeat, not the step table of each:\n%s", out)
	}
	if header > progress {
		t.Fatalf("the slice header must come before the step progress it introduces:\n%s", out)
	}
	if strings.Count(out, "slice of cli-noisy-flow for step fetch") != 1 {
		t.Fatalf("the header must be printed once:\n%s", out)
	}
}
