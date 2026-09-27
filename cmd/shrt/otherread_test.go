package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAReadAnsweringFewerLinesNamesAnotherReadOfTheSameRecord(t *testing.T) {
	three := `{"id_order":"o1","id_customer":"c1","lines":[{"id_product":"p1"},{"id_product":"p1"},{"id_product":"p2"}]}`
	one := `{"id_order":"o1","id_customer":"c1","lines":[{"id_product":"p1"}]}`
	list := `{"orders":[` + three + `]}`
	rec := func(listed string) []recStep {
		fetch := shopStep("fetch_order", shopFetch, `{"order":`+one+`}`, "create_order").failing("order.lines.1.id_product", "p1", nil)
		fetch.Expect = append(fetch.Expect, chain.ExpectResult{Path: "order.lines.0.id_product", Rule: "equals", Want: "p1", Got: "p1", Passed: true})
		return []recStep{
			shopStep("create_order", shopOrder, `{"order":`+three+`}`),
			fetch,
			shopStep("list_orders", "shop.orders.v1.OrderService/ListOrders", listed, "create_order"),
		}
	}
	_, own, _ := blameOf(t, shopRecord(rec(list)...), "fetch_order", "order.lines.1.id_product")
	if !strings.HasSuffix(own, "; ListOrders read the same order with 3 lines") {
		t.Errorf("another read holding the lines clears the write, got %q", own)
	}
	_, own, _ = blameOf(t, shopRecord(rec(`{"orders":[`+one+`]}`)...), "fetch_order", "order.lines.1.id_product")
	if !strings.HasSuffix(own, "; ListOrders read the same order with 1 lines too") {
		t.Errorf("every read answering the same set points at the write, got %q", own)
	}
	_, own, _ = blameOf(t, shopRecord(rec(`{"orders":[]}`)...), "fetch_order", "order.lines.1.id_product")
	if strings.Contains(own, "read the same") {
		t.Errorf("no other read of the order, nothing to add: %q", own)
	}
}

func TestAGroupShowsTheReasonThatNamesAnotherRead(t *testing.T) {
	gr := &gateGroup{}
	base := "FetchOrder answers another set of order.lines"
	gr.addOwn(base)
	gr.addOwn(base + "; ListOrders read the same order with 3 lines")
	gr.addOwn(base)
	if len(gr.own) != 1 || !strings.HasSuffix(gr.own[0], "3 lines") {
		t.Errorf("one reason, the fuller one: %q", gr.own)
	}
}
