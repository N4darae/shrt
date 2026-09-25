package store_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/store"
)

func TestTheSentColumnShowsLiteralValuesInFullWhateverTheirLength(t *testing.T) {
	rec := passingRun("run-2")
	rec.Steps[0].Request = json.RawMessage(`{"email":"cust-order-confirm-long@example.test","name":"Customer order-confirm-long","id_customer":"cus-1234567890abcdef1234"}`)
	p := &store.Proposal{Chain: rec.Chain, RunID: rec.RunID}
	text := store.ProposalSummary(p, rec)
	for _, want := range []string{"email=cust-order-confirm-long@example.test", "name=Customer order-confirm-long"} {
		if !strings.Contains(text, want) {
			t.Errorf("a literal input is clipped by its length, so a short fixture shows in full and a long one does not; want %q in:\n%s", want, text)
		}
	}
}
