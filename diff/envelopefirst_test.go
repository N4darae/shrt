package diff_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
)

func TestAStepsEnvelopeChangeIsListedFirst(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	spot := spotOf(nil, step("get", `{"customer":null,"status":{"code":"REJECTED"}}`))
	rec := recOf(step("get", `{"customer":{"name":""},"status":{"code":"SUCCESS"}}`))
	rep := diff.Compare(spot, rec)
	if len(rep.Changes) < 2 || rep.Changes[0].Path != "status.code" {
		t.Fatalf("the envelope verdict flip leads the step's changes, got %+v", rep.Changes)
	}
}
