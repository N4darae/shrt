package hollow_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/hollow"
)

func TestHollowFollowsTheConfiguredEnvelopeRatherThanTheWordError(t *testing.T) {
	defer chain.SetEnvelope("", "")

	if !hollow.IsEnvelopePath("error.code") {
		t.Fatal("the default envelope is not recognised")
	}

	chain.SetEnvelope("status.code", "SUCCESS")
	if !hollow.IsEnvelopePath("status.code") {
		t.Error("a step asserting only the relocated envelope is counted as asserting DATA, so no read is " +
			"ever envelope-only, so 'shrt chain hollow' reports 0 and passes forever — a gate that silently " +
			"stopped gating")
	}
	if hollow.IsEnvelopePath("error.code") {
		t.Error("error.code still reads as the envelope after it was moved, so a real data assertion on a " +
			"field named error would be discounted")
	}
}
