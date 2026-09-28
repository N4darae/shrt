package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAWriteOnAnOrderActsOnTheProductsItsLinesName(t *testing.T) {
	c := &chain.Chain{Name: "stock", Steps: []*chain.Step{
		{ID: "create_product", Call: "ProductService/CreateProduct", Body: map[string]any{"sku": "a"}},
		{ID: "create_order", Call: "OrderService/CreateOrder", Body: map[string]any{"lines": []any{map[string]any{"id_product": "${create_product.product.id_product}"}}}},
		{ID: "confirm_order", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id_order": "ord-1"}},
		{ID: "cancel_order", Call: "OrderService/CancelOrder", Body: map[string]any{"id_order": "ord-1"}},
		{ID: "get_product", Call: "ProductService/GetProduct", Body: map[string]any{"id_product": "${create_product.product.id_product}"}},
	}}
	step := func(id, call, req, resp string) *runner.StepRecord {
		return &runner.StepRecord{ID: id, Call: call, Status: runner.StatusPassed, HTTPStatus: 200, Request: []byte(req), Response: []byte(resp)}
	}
	order := `{"order":{"id_order":"ord-1","lines":[{"id_product":"prd-1","qty":"2"}]}}`
	rec := &runner.Record{RunID: "src", Chain: "stock", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		step("create_product", "ProductService/CreateProduct", `{"sku":"a"}`, `{"product":{"id_product":"prd-1","sku":"a"}}`),
		step("create_order", "OrderService/CreateOrder", `{"lines":[{"id_product":"prd-1"}]}`, order),
		step("confirm_order", "OrderService/ConfirmOrder", `{"id_order":"ord-1"}`, order),
		step("cancel_order", "OrderService/CancelOrder", `{"id_order":"ord-1"}`, order),
		step("get_product", "ProductService/GetProduct", `{"id_product":"prd-1"}`, `{"product":{"id_product":"prd-1","qty_on_hand":"8"}}`),
	}}
	prereqs := func(rpc string) []chain.Prereq {
		if rpc == "OrderService/ConfirmOrder" {
			return []chain.Prereq{{RPC: "StockService/AddStock", Edge: "needs"}}
		}
		return nil
	}
	res, err := chain.Slice(c, "get_product", chain.SliceOptions{RunID: rec.RunID, Refused: refusedIn(rec), Prereqs: prereqs})
	if err != nil {
		t.Fatal(err)
	}
	related, other := relatedDroppedWrites(res, rec)
	if !slices.Contains(related, "confirm_order") {
		t.Fatalf("confirm_order changes an existing order and, as its contract needs AddStock, the stock of the product on its line, which the target reads: related %v, other %v", related, other)
	}
}

func TestDroppedWriteListsAreCappedAtFiveIDs(t *testing.T) {
	ids := []string{"w1", "w2", "w3", "w4", "w5", "w6", "w7"}
	res := &chain.SliceResult{Target: "t", Total: 9, Reach: 8, UnderIncluded: true}
	for i, id := range ids {
		res.DroppedWrites = append(res.DroppedWrites, chain.Dropped{Index: i + 1, ID: id})
	}
	v := &sliceVerdict{Outcome: sliceReproduced, OtherDropped: ids}
	out := captureStdout(t, func() { printSlice(res, "", v, false) })
	if strings.Contains(out, "w7") || strings.Contains(out, "WARNING") {
		t.Fatalf("a reproduced slice does not list unrelated dropped writes:\n%s", out)
	}
	out = captureStdout(t, func() { printSlice(res, "", nil, false) })
	if !strings.Contains(out, "w1, w2, w3 and 4 more") || !strings.Contains(out, "WARNING") {
		t.Fatalf("an unverified slice names three dropped writes and counts the rest:\n%s", out)
	}
}
