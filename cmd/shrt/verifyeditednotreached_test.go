package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestAStepNotReachedAfterAnEditedExpectationIsNotARegression(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "ids and names checked"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	path := ".shrt/chains/cli-thing-flow.yaml"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "          - path: id\n            not_empty: true", "          - path: id\n            equals: nope", 1)
	if edited == string(raw) {
		t.Fatal("the chain fixture changed; the edit no longer applies")
	}
	writeFile(t, path, edited)
	var verr error
	out := captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-save=false"}) })
	if verr == nil {
		t.Fatalf("an edited expectation that fails is drift:\n%s", out)
	}
	if strings.Contains(verr.Error(), "regression") {
		t.Fatalf("the step the edited expectation stopped, and every step not reached after it, follow the chain change; "+
			"none is evidence of a backend regression, got %v:\n%s", verr, out)
	}
}
