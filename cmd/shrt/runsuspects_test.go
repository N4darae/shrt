package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAKeepGoingRunNamesEachDistinctSuspectMostFailingStepsFirst(t *testing.T) {
	list := shopStep("list_cancelled", "shop.orders.v1.OrderService/ListOrders", `{"orders":[{"id_order":"o3"},{"id_order":"o2"},{"id_order":"o1"}]}`).failing("orders.0.id_order", "o2", "o3")
	list.Expect = append(list.Expect, chain.ExpectResult{Path: "orders.1", Rule: "exists", Want: false, Got: true})
	rec := shopRecord(
		shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`).failing("product.sku", "A", nil),
		list,
		shopStep("get_after_add", shopGet, `{"product":{"id_product":"p1"}}`, "create").heldBy("create", "product.sku"),
		shopStep("get_again", shopGet, `{"product":{"id_product":"p1"}}`, "create").heldBy("create", "product.sku"),
	)
	for _, st := range rec.Steps {
		st.Request = json.RawMessage(`{"n":1}`)
	}
	rec.Status, rec.KeepGoing = runner.StatusFailed, true
	lines := failureRequests(nil, rec, false)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "suspect write create (ProductService/CreateProduct)") ||
		!strings.HasPrefix(lines[1], "suspect read list_cancelled (OrderService/ListOrders)") {
		t.Fatalf("one line per distinct suspect, the one behind most failing steps first, got %q", lines)
	}
	if failureRequests(nil, rec, true) != nil {
		t.Fatal("a dry run sent nothing, so it names no request")
	}
}
