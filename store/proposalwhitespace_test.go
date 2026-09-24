package store_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestProposalSummaryQuotesAnEmptyOrSpacePaddedValue(t *testing.T) {
	rec := &runner.Record{
		RunID: "run-w", Chain: "ws", Target: "http://localhost", Status: runner.StatusPassed,
		Steps: []*runner.StepRecord{{
			Index: 1, ID: "create", Call: "ProductService/CreateProduct", Status: runner.StatusPassed,
			Request:  json.RawMessage(`{"sku":" ","name":"","note":" padded","plain":"widget two"}`),
			Response: json.RawMessage(`{}`),
		}},
	}
	text := store.ProposalSummary(&store.Proposal{Chain: rec.Chain, RunID: rec.RunID}, rec)
	for _, want := range []string{`sku=" "`, `name=""`, `note=" padded"`, `plain=widget two`} {
		if !strings.Contains(text, want) {
			t.Errorf("the sent column must show %s so whitespace is not hidden:\n%s", want, text)
		}
	}
}
