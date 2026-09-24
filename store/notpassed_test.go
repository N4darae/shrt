package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestProposingAFailedRunNamesTheFailingStep(t *testing.T) {
	s := newStore(t)
	failed := passingRun("run-f")
	failed.Status = runner.StatusFailed
	failed.Steps[len(failed.Steps)-1].Status = runner.StatusFailed
	last := failed.Steps[len(failed.Steps)-1].ID
	_, err := s.Propose(failed, store.ProposalInput{Checked: "x"})
	if !errors.Is(err, store.ErrRunNotPassed) {
		t.Fatalf("a failed run cannot be proposed, got %v", err)
	}
	if !strings.Contains(err.Error(), last+" (failed)") || !strings.Contains(err.Error(), "run-f") {
		t.Fatalf("the refusal must name the run and the step that failed: %v", err)
	}
}
