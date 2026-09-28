package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestASliceFailureReadingAnOrderNoStepCreatedIsACaveat(t *testing.T) {
	create := &runner.StepRecord{ID: "create_order", Status: runner.StatusPassed,
		Request:  json.RawMessage(`{"id_customer":"cus-111111111111"}`),
		Response: json.RawMessage(`{"order":{"id_order":"ord-aaaaaaaaaaa1","id_customer":"cus-111111111111"}}`)}
	list := &runner.StepRecord{ID: "list_orders_cancelled", Status: runner.StatusFailed,
		Request: json.RawMessage(`{"id_customer":"cus-111111111111","status":"ORDER_STATUS_CANCELLED"}`),
		Response: json.RawMessage(`{"orders":[{"id_order":"ord-ffffffffff99","id_customer":"cus-999999999999","status":"ORDER_STATUS_CANCELLED"},` +
			`{"id_order":"ord-aaaaaaaaaaa1","id_customer":"cus-111111111111","status":"ORDER_STATUS_PENDING"}]}`),
		Expect: []chain.ExpectResult{
			{Path: "orders.0.id_order", Rule: "equals", Want: "ord-aaaaaaaaaaa1", Got: "ord-ffffffffff99"},
			{Path: "orders.1", Rule: "exists", Want: false, Got: true},
		}}
	run := &runner.Record{RunID: "slice", Steps: []*runner.StepRecord{create, list}}
	got := outsideState(run, list)
	joined := strings.Join(got, "; ")
	for _, want := range []string{"orders.0.id_order ord-ffffffffff99", "orders.0.id_customer cus-999999999999"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the other customer's cancelled order came from outside the slice, want %q in %v", want, got)
		}
	}
	if strings.Contains(joined, "ord-aaaaaaaaaaa1") || strings.Contains(joined, "cus-111111111111") {
		t.Errorf("an order the slice created is not outside state: %v", got)
	}
	if line := outsideStateCaveat(list.ID, got); !strings.Contains(line, "fails on server state the slice did not create") {
		t.Errorf("the caveat says so: %s", line)
	}
	list.Response = json.RawMessage(`{"orders":[{"id_order":"ord-aaaaaaaaaaa1","id_customer":"cus-111111111111","status":"ORDER_STATUS_PENDING"},{"id_order":"ord-aaaaaaaaaaa1"}]}`)
	list.Expect = []chain.ExpectResult{{Path: "orders.1", Rule: "exists", Want: false, Got: true}}
	if got := outsideState(run, list); len(got) != 0 {
		t.Errorf("a failure over what the slice created carries no caveat: %v", got)
	}
}

func TestAnIdTheFailingWriteItselfAnsweredIsNotOutsideState(t *testing.T) {
	create := &runner.StepRecord{ID: "create_order", Call: "shop.orders.v1.OrderService/CreateOrder", Status: runner.StatusPassed,
		Request:  json.RawMessage(`{"idempotency_key":"k-1"}`),
		Response: json.RawMessage(`{"order":{"id_order":"ord-aaaaaaaaaaa1"}}`)}
	replay := &runner.StepRecord{ID: "create_order_replay", Call: "shop.orders.v1.OrderService/CreateOrder", Status: runner.StatusFailed,
		Request:  json.RawMessage(`{"idempotency_key":"k-1"}`),
		Response: json.RawMessage(`{"order":{"id_order":"ord-bbbbbbbbbbb2"}}`),
		Expect:   []chain.ExpectResult{{Path: "order.id_order", Rule: "equals", Want: "ord-aaaaaaaaaaa1", Got: "ord-bbbbbbbbbbb2"}}}
	run := &runner.Record{RunID: "slice", Steps: []*runner.StepRecord{create, replay}}
	if got := outsideState(run, replay); len(got) != 0 {
		t.Errorf("the replay created ord-bbbbbbbbbbb2 itself, inside the slice: %v", got)
	}
	replay.Call = "shop.orders.v1.OrderService/FetchOrder"
	if got := outsideState(run, replay); len(got) != 1 {
		t.Errorf("a read answering an id no step sent or received did not create it: %v", got)
	}
}
