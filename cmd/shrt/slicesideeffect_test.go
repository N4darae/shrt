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
		{ID: "confirm_order", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id_order": "${create_order.order.id_order}"}},
		{ID: "cancel_order", Call: "OrderService/CancelOrder", Body: map[string]any{"id_order": "${create_order.order.id_order}"}},
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
	if note := otherEntitiesNote(other); strings.Contains(note, "confirm_order") {
		t.Fatalf("never say the confirm changes no entity a kept step uses: %s", note)
	}
}
