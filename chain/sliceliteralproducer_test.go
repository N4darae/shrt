package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func literalProducerFixture() *chain.Chain {
	return &chain.Chain{
		Name: "slice-repro",
		Steps: []*chain.Step{
			{ID: "good_customer", Call: "CustomerService/CreateCustomer", Body: map[string]any{"email": "a@example.test"},
				Expect: []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}},
			{ID: "bad_customer", Call: "CustomerService/CreateCustomer", Body: map[string]any{"email": "no-at-sign"},
				Expect: []chain.Expectation{{Path: "transport.code", Equals: "invalid_argument"}}},
			{ID: "list_literal", Call: "OrderService/ListOrders", Body: map[string]any{"id_customer": "cus-literal"}},
			{ID: "list_ref", Call: "OrderService/ListOrders", Body: map[string]any{"id_customer": "${vars.who}"}},
			{ID: "list_unset", Call: "OrderService/ListOrders"},
		},
	}
}

func literalProducerPrereqs(rpc string) []chain.Prereq {
	if rpc == "OrderService/ListOrders" {
		return []chain.Prereq{{RPC: "CustomerService/CreateCustomer", Edge: "from", Field: "id_customer"}}
	}
	return nil
}

func TestSliceNeedsNoProducerForALiteralField(t *testing.T) {
	for _, target := range []string{"list_literal", "list_ref"} {
		res, err := chain.Slice(literalProducerFixture(), target, chain.SliceOptions{Prereqs: literalProducerPrereqs})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(keptIDs(res), ","); got != target {
			t.Fatalf("%s: kept %s, want only the target: its id_customer is not produced by any step", target, got)
		}
		if len(res.Unmet) != 0 {
			t.Fatalf("%s: unmet %+v, want none for a literal field", target, res.Unmet)
		}
	}
}

func TestSliceNeverPicksARefusedStepAsProducer(t *testing.T) {
	res, err := chain.Slice(literalProducerFixture(), "list_unset", chain.SliceOptions{Prereqs: literalProducerPrereqs})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(keptIDs(res), ","), "good_customer,list_unset"; got != want {
		t.Fatalf("kept %s, want %s: bad_customer expects a refusal and creates nothing", got, want)
	}
	full := literalProducerFixture()
	onlyRefused := &chain.Chain{Name: full.Name, Steps: []*chain.Step{full.Steps[1], full.Steps[4]}}
	res, err = chain.Slice(onlyRefused, "list_unset", chain.SliceOptions{Prereqs: literalProducerPrereqs})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(keptIDs(res), ","); got != "list_unset" || len(res.Unmet) != 1 {
		t.Fatalf("kept %s unmet %+v, want the refused step left out and the prerequisite unmet", got, res.Unmet)
	}
	withRun := literalProducerFixture()
	withRun.Steps[1].Expect = nil
	res, err = chain.Slice(withRun, "list_unset", chain.SliceOptions{Prereqs: literalProducerPrereqs,
		Refused: func(id string) (string, bool) { return "refused: transport invalid_argument", id == "bad_customer" }})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(keptIDs(res), ","), "good_customer,list_unset"; got != want {
		t.Fatalf("kept %s, want %s: bad_customer was refused in the run", got, want)
	}
}
