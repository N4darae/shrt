package main

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAMovedPinOnAListWhoseOtherPinHeldIsAReorder(t *testing.T) {
	const list = "shop.orders.v1.OrderService/ListOrders"
	st := shopStep("list_cancelled", list, `{"orders":[{"id_order":"o3"},{"id_order":"o2"},{"id_order":"o1"}]}`).failing("orders.0.id_order", "o2", "o3")
	st.Expect = append(st.Expect, chain.ExpectResult{Path: "orders.1", Rule: "exists", Want: false, Got: true})
	rec := shopRecord(st)
	if own := runAttribution(nil, rec).of("list_cancelled", "orders.0.id_order").Kind; own != reasonSet {
		t.Fatalf("judged against its expectations, the extra item is another set, got %q", own)
	}
	held := map[string]bool{"list_cancelled orders.1": true}
	if own := pinnedAttribution(nil, rec, held).of("list_cancelled", "orders.0.id_order").Kind; own != reasonOrder {
		t.Fatalf("the extra item is the pinned defect, unchanged, so what moved is the order, got %q", own)
	}
}
