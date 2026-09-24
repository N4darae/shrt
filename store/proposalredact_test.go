package store_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestProposalSummaryNamesRedactedResponseFieldsAsNeverCompared(t *testing.T) {
	rec := &runner.Record{
		RunID: "run-r", Chain: "red", Target: "http://localhost", Status: runner.StatusPassed,
		Redacted: []string{"**.qty_on_hand"},
		Steps: []*runner.StepRecord{{
			Index: 1, ID: "get_product", Call: "ProductService/GetProduct", Status: runner.StatusPassed,
			Request:  json.RawMessage(`{"id_product":"prd-1"}`),
			Response: json.RawMessage(`{"product":{"qty_on_hand":"<redacted>","name":"Widget"}}`),
		}},
	}
	text := store.ProposalSummary(&store.Proposal{Chain: rec.Chain, RunID: rec.RunID}, rec)
	if !strings.Contains(text, "Redacted, never compared by `shrt verify`") || !strings.Contains(text, "get_product product.qty_on_hand") {
		t.Fatalf("a redacted response field is blanked in the record, so verify can never see it change; "+
			"the user approving the baseline must be told which ones:\n%s", text)
	}
}
