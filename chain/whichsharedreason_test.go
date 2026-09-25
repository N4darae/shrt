package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func sharedReasonFixture() []*chain.Chain {
	return []*chain.Chain{{
		Name: "customers",
		Steps: []*chain.Step{
			{ID: "get_customer_missing", Call: "shop.v1.CustomerService/GetCustomer", Expect: []chain.Expectation{
				{Path: "status.details.0.app_code", Equals: 1102},
				{Path: "status.details.0.reason", Equals: "CustomerNotFound"},
			}},
			{ID: "create_order_unknown_customer", Call: "shop.v1.OrderService/CreateOrder", Expect: []chain.Expectation{
				{Path: "status.details.0.app_code", Equals: 1301},
				{Path: "status.details.0.reason", Equals: "CustomerNotFound"},
			}},
			{ID: "list_orders_unknown_customer", Call: "shop.v1.OrderService/ListOrders", Expect: []chain.Expectation{
				{Path: "status.details.0.reason", Equals: "CustomerNotFound"},
			}},
		},
	}}
}

func TestWhichCodeDoesNotMatchAnotherCodeThatSharesTheReason(t *testing.T) {
	q := chain.WhichQuery{Code: "1102", Aliases: []string{"CustomerNotFound"}}
	hits := chain.Which(sharedReasonFixture(), q, chain.WhichOptions{})
	if len(hits) != 1 {
		t.Fatalf("want one chain, got %+v", hits)
	}
	got := map[string]chain.WhichStep{}
	for _, m := range hits[0].Matches {
		got[m.Step] = m
	}
	if _, ok := got["create_order_unknown_customer"]; ok {
		t.Fatalf("create_order_unknown_customer asserts app_code 1301, not 1102, so the shared reason does not make it a match: %+v", hits[0].Matches)
	}
	if m, ok := got["get_customer_missing"]; !ok || m.ByReason != "" {
		t.Fatalf("the step asserting 1102 matches on the code: %+v", hits[0].Matches)
	}
	if m, ok := got["list_orders_unknown_customer"]; !ok || m.ByReason != "CustomerNotFound" {
		t.Fatalf("a step asserting only the reason matches by the reason, and says so: %+v", hits[0].Matches)
	}
}

func TestWhichCodeByReasonStillMatchesEveryCodeOfThatReason(t *testing.T) {
	q := chain.WhichQuery{Code: "CustomerNotFound", Aliases: []string{"1102", "1301"}}
	hits := chain.Which(sharedReasonFixture(), q, chain.WhichOptions{})
	if len(hits) != 1 || len(hits[0].Matches) != 3 {
		t.Fatalf("-code CustomerNotFound asks for the reason, which all three assert: %+v", hits)
	}
}
