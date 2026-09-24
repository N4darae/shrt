package store_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestProposeWritesAReportAndNoSafeSpot(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-1")
	if _, err := s.SaveRun(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Propose(rec, store.ProposalInput{}); !errors.Is(err, store.ErrNoEvidence) {
		t.Fatalf("a proposal must say what was checked, got %v", err)
	}
	failed := passingRun("run-f")
	failed.Status = runner.StatusFailed
	if _, err := s.Propose(failed, store.ProposalInput{Checked: "x"}); !errors.Is(err, store.ErrRunNotPassed) {
		t.Fatalf("a failed run cannot be proposed, got %v", err)
	}
	p, err := s.Propose(rec, store.ProposalInput{Checked: "id is a fresh thing id"})
	if err != nil {
		t.Fatal(err)
	}
	if s.HasSafeSpot(rec.Chain) {
		t.Fatal("proposing must not write a safe spot")
	}
	if p.ProposedBy != "agent" {
		t.Fatalf("an unnamed proposer is the agent, got %q", p.ProposedBy)
	}
	raw, err := os.ReadFile(p.Report)
	if err != nil || !strings.Contains(string(raw), "id is a fresh thing id") || !strings.Contains(string(raw), `"id": "thing-1"`) {
		t.Fatalf("the report must carry the proposer's evidence and the responses: %v\n%s", err, raw)
	}
}

func TestApprovePromotesTheProposedRunAndRecordsWhoProposed(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-1")
	if _, err := s.SaveRun(rec); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Approve(rec.Chain, humanConfirmation()); !errors.Is(err, store.ErrNoProposal) {
		t.Fatalf("nothing to approve without a proposal, got %v", err)
	}
	if _, err := s.Propose(rec, store.ProposalInput{Checked: "checked"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Approve(rec.Chain, store.Confirmation{By: "reviewer"}); !errors.Is(err, store.ErrNotConfirmed) {
		t.Fatalf("approval still needs a person's acknowledgement, got %v", err)
	}
	spot, _, err := s.Approve(rec.Chain, store.Confirmation{By: "reviewer", Acknowledged: true})
	if err != nil {
		t.Fatal(err)
	}
	if spot.ConfirmedBy != "reviewer" || spot.ProposedBy != "agent" || spot.Note != "checked" {
		t.Fatalf("the safe spot must record both the approver and the proposer, got %+v", spot)
	}
	if s.HasProposal(rec.Chain) {
		t.Fatal("an approved proposal must be cleared")
	}
}

func TestApproveRefusesARunRewrittenAfterTheProposal(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-1")
	if _, err := s.SaveRun(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Propose(rec, store.ProposalInput{Checked: "checked"}); err != nil {
		t.Fatal(err)
	}
	rec.Steps[0].Response = []byte(`{"id":"thing-2"}`)
	if _, err := s.SaveRun(rec); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Approve(rec.Chain, humanConfirmation()); !errors.Is(err, store.ErrProposalChanged) {
		t.Fatalf("the person approves what the report showed, not a later rewrite, got %v", err)
	}
	if s.HasSafeSpot(rec.Chain) {
		t.Fatal("a refused approval must not write a safe spot")
	}
}

func TestProposalSummaryWarnsAboutFieldsThatWillDrift(t *testing.T) {
	rec := passingRun("run-2")
	p := &store.Proposal{Chain: rec.Chain, RunID: rec.RunID, ComparedTo: "run-1", Unstable: []string{"create product.sku"}}
	text := store.ProposalSummary(p, rec)
	if !strings.Contains(text, "Warning: 1 field(s) differ from the earlier passing run `run-1`") || !strings.Contains(text, "`create product.sku`") {
		t.Fatalf("the summary must name the fields verify would report:\n%s", text)
	}
	p.ComparedTo, p.Unstable = "", nil
	if !strings.Contains(store.ProposalSummary(p, rec), "Not checked for fields that change every run") {
		t.Fatal("without an earlier run the summary must say the check was not made")
	}
}
