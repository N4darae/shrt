package main

import (
	"slices"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func listedChildCase(t *testing.T) (*chain.SliceResult, *runner.Record) {
	t.Helper()
	c := &chain.Chain{Name: "orders-list", Steps: []*chain.Step{
		{ID: "create_customer", Call: "CustomerService/CreateCustomer", Body: map[string]any{"email": "c@example.test"}},
		{ID: "create_customer_2", Call: "CustomerService/CreateCustomer", Body: map[string]any{"email": "d@example.test"}},
		{ID: "create_order", Call: "OrderService/CreateOrder", Body: map[string]any{"id_customer": "${create_customer.customer.id_customer}"}},
		{ID: "create_order_other", Call: "OrderService/CreateOrder", Body: map[string]any{"id_customer": "${create_customer_2.customer.id_customer}"}},
		{ID: "create_order_2", Call: "OrderService/CreateOrder", Body: map[string]any{"id_customer": "${create_customer.customer.id_customer}"}},
		{ID: "list_orders", Call: "OrderService/ListOrders", Body: map[string]any{"id_customer": "${create_customer.customer.id_customer}"},
			Expect: []chain.Expectation{
				{Path: "orders.0.id_order", Equals: "${create_order.order.id_order}"},
				{Path: "orders.1.id_order", Equals: "${create_order_2.order.id_order}"},
			}},
	}}
	step := func(id, call, req, resp string, expect ...chain.ExpectResult) *runner.StepRecord {
		status := runner.StatusPassed
		for _, e := range expect {
			if !e.Passed {
				status = runner.StatusFailed
			}
		}
		return &runner.StepRecord{ID: id, Call: call, Status: status, HTTPStatus: 200, Request: []byte(req), Response: []byte(resp), Expect: expect}
	}
	rec := &runner.Record{RunID: "src", Chain: "orders-list", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		step("create_customer", "CustomerService/CreateCustomer", `{"email":"c@example.test"}`, `{"customer":{"id_customer":"cus-a"}}`),
		step("create_customer_2", "CustomerService/CreateCustomer", `{"email":"d@example.test"}`, `{"customer":{"id_customer":"cus-b"}}`),
		step("create_order", "OrderService/CreateOrder", `{"id_customer":"cus-a"}`, `{"order":{"id_order":"ord-1","id_customer":"cus-a"}}`),
		step("create_order_other", "OrderService/CreateOrder", `{"id_customer":"cus-b"}`, `{"order":{"id_order":"ord-9","id_customer":"cus-b"}}`),
		step("create_order_2", "OrderService/CreateOrder", `{"id_customer":"cus-a"}`, `{"order":{"id_order":"ord-2","id_customer":"cus-a"}}`),
		step("list_orders", "OrderService/ListOrders", `{"id_customer":"cus-a"}`, `{"orders":[{"id_order":"ord-1","id_customer":"cus-a"}]}`,
			chain.ExpectResult{Path: "orders.0.id_order", Rule: "equals", Want: "ord-1", Got: "ord-1", Passed: true},
			chain.ExpectResult{Path: "orders.1.id_order", Rule: "equals", Want: "ord-2", Passed: false, Detail: "path not present in response"}),
	}}
	res, err := chain.Slice(c, "list_orders", chain.SliceOptions{Mode: chain.SliceModePin, RunID: rec.RunID,
		Value: recordValues(rec), RunVars: recordVars(rec), Refused: refusedIn(rec), Performed: performedIn(rec)})
	if err != nil {
		t.Fatal(err)
	}
	return res, rec
}

func TestAWriteCreatingAnItemTheListedTargetReturnsIsRelated(t *testing.T) {
	res, rec := listedChildCase(t)
	related, other := relatedDroppedWrites(res, rec)
	if !slices.Contains(related, "create_order_2") {
		t.Fatalf("create_order_2 creates an order for the customer list_orders lists, so it acts on what the target returns: related %v, other %v", related, other)
	}
	if slices.Contains(related, "create_order_other") {
		t.Fatalf("create_order_other is another customer's order, which the list does not return: related %v", related)
	}
}

func TestAPinnedFailureAboutAnItemTheSliceNeverCreatesIsNoReceipt(t *testing.T) {
	res, rec := listedChildCase(t)
	source, _ := rec.Step("list_orders")
	if got := uncreatedExpected(res, source); !slices.Contains(got, "create_order_2") {
		t.Fatalf("orders.1.id_order failed against the id create_order_2 created in the source run, and the pinned slice never sends create_order_2, so a match there proves nothing: %v", got)
	}
}
