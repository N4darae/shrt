package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAReferenceToAPathTheAnsweredStepLacksFailsTheReferencingStep(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := &chain.Chain{
		Name: "missing-ref-path",
		Steps: []*chain.Step{
			{ID: "login", Call: "AuthService/Login", SkipAuth: true,
				Body: map[string]any{"username": "alice", "password": "hunter2"}},
			{ID: "preview", Call: "BatchService/Preview", Body: map[string]any{"lines": []any{}}},
			{ID: "use", Call: "BatchService/Preview", Body: map[string]any{"lines": []any{"${preview.results.0.amount}"}}},
		},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	rec, err := newBatchRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Status != runner.StatusFailed {
		t.Fatalf("a path the recorded response lacks will be lacking on every re-run, so the run is failed, not %s: %s", rec.Status, rec.Failure)
	}
	use, _ := rec.Step("use")
	want := `step "preview" answered without results.0.amount (results is [])`
	if use.Status != runner.StatusFailed || !strings.Contains(use.Error, want) {
		t.Fatalf("use: %s %q, want failed naming %q", use.Status, use.Error, want)
	}
}
