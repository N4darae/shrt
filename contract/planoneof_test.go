package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func TestPlannedBodyArmsOnlyTheOneofMemberTheContractNames(t *testing.T) {
	lib := libraryFrom(t, `
domain: rich
rpcs:
  shrt.test.rich.v1.OrderService/PlaceOrder:
    summary: place an order
    fields:
      bank_ref:
        oneof: payment
        value: REF-1
    status: draft
`)
	cat := catalogtest.Rich()
	plan, err := contract.BuildPlan("shrt.test.rich.v1.OrderService/PlaceOrder", lib, cat, "rich-plan")
	if err != nil {
		t.Fatal(err)
	}
	step := plan.Chain.Steps[len(plan.Chain.Steps)-1]
	if step.Body["bank_ref"] != "REF-1" {
		t.Fatalf("the armed member must carry its value: %v", step.Body)
	}
	if _, ok := step.Body["card_token"]; ok {
		t.Fatalf("the group's first member must step aside for the armed one: %v", step.Body)
	}
	raw, err := json.Marshal(step.Body)
	if err != nil {
		t.Fatal(err)
	}
	m, err := cat.Lookup("shrt.test.rich.v1.OrderService/PlaceOrder")
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.ValidateInput(m, raw); err != nil {
		t.Fatalf("a planned body must validate against its own request message: %v\n%s", err, raw)
	}
}
