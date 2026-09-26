package store_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestApproveRefusesAnyEditToWhatBecomesTheSafeSpot(t *testing.T) {
	edits := map[string]func(*runner.Record){
		"volatile":        func(r *runner.Record) { r.Volatile = []string{"**"} },
		"step volatile":   func(r *runner.Record) { r.Steps[0].Volatile = []string{"**.id"} },
		"step status":     func(r *runner.Record) { r.Steps[0].Status = runner.StatusFailed },
		"request":         func(r *runner.Record) { r.Steps[0].Request = json.RawMessage(`{"name":"gadget"}`) },
		"target":          func(r *runner.Record) { r.Target = "http://elsewhere" },
		"transport_error": func(r *runner.Record) { r.Steps[0].Transport = &runner.TransportError{Code: "unavailable"} },
		"http_status":     func(r *runner.Record) { r.Steps[0].HTTPStatus = 503 },
		"vars":            func(r *runner.Record) { r.Vars = map[string]any{"tag": "t2"} },
		"build":           func(r *runner.Record) { r.Build = "other" },
	}
	for name, edit := range edits {
		s := newStore(t)
		rec := passingRun("run-1")
		rec.Steps[0].Request = json.RawMessage(`{"name":"widget"}`)
		rec.Steps[0].HTTPStatus = 200
		rec.Vars = map[string]any{"tag": "t1"}
		if _, err := s.SaveRun(rec); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Propose(rec, store.ProposalInput{Checked: "checked"}); err != nil {
			t.Fatal(err)
		}
		edit(rec)
		if _, err := s.SaveRun(rec); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Approve(rec.Chain, humanConfirmation()); !errors.Is(err, store.ErrProposalChanged) {
			t.Errorf("%s edited after proposing: approval must be refused, got %v", name, err)
		}
	}
}

func TestProposalSummaryListsVolatileAndNamesFullyMaskedSteps(t *testing.T) {
	rec := passingRun("run-1")
	rec.Steps = append(rec.Steps, &runner.StepRecord{Index: 2, ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusPassed,
		Response: json.RawMessage(`{"thing":{"total":"500"},"status":{"code":"OK"}}`)})
	rec.Volatile = []string{"**.sku"}
	rec.Steps[1].Volatile = []string{"thing", "status.code"}
	p := &store.Proposal{Chain: rec.Chain, RunID: rec.RunID}
	text := store.ProposalSummary(p, rec)
	for _, want := range []string{"`**.sku`", "`thing`", "`status.code`", "every response field of step(s) fetch"} {
		if !strings.Contains(text, want) {
			t.Errorf("summary must say %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "step(s) create,") || strings.Contains(text, "fetch, create") {
		t.Errorf("create has an unmasked field:\n%s", text)
	}
	rec.Volatile = []string{"**"}
	text = store.ProposalSummary(p, rec)
	if !strings.Contains(text, "every response field of step(s) create, fetch") {
		t.Errorf("`**` masks every step:\n%s", text)
	}
}
