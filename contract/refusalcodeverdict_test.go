package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func TestAStepAssertingARefusalCodeIsNotASuccessAssertingOnlyTheVerdict(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope(chain.DefaultEnvelopePath, chain.DefaultEnvelopeOK)
	refusal := &chain.Step{ID: "confirm_twice", Call: "shop.orders.v1.OrderService/ConfirmOrder", Expect: []chain.Expectation{
		{Path: "status.details.0.app_code", Equals: 1102},
	}}
	if contract.AssertsOnlyVerdict(refusal) {
		t.Fatalf("app_code equals 1102 names a refusal, so the step does not expect success")
	}
	success := &chain.Step{ID: "confirm", Call: "shop.orders.v1.OrderService/ConfirmOrder", Expect: []chain.Expectation{
		{Path: "status.code", Equals: "SUCCESS"},
	}}
	if !contract.AssertsOnlyVerdict(success) {
		t.Fatalf("a step asserting only status.code equals SUCCESS asserts only the verdict")
	}
}
