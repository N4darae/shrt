package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAReadHeldBehindAHeldReadNamesTheWriteThatLostTheField(t *testing.T) {
	rec := shopRecord(
		shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`).failing("product.created_at", "t", nil),
		shopStep("get", shopGet, `{"product":{"id_product":"p1","created_at":"t"}}`, "create").heldBy("create", "product.created_at"),
		shopStep("get_as_clerk", shopGet, `{"product":{"id_product":"p1","created_at":"t"}}`, "create").heldBy("get", "product.created_at"),
	)
	write, own, cascade := blameOf(t, rec, "get_as_clerk", "status")
	if write != "create" || own != "" || cascade != reasonKnockOn {
		t.Errorf("got write %q own %q cascade %q", write, own, cascade)
	}
}

func TestALaterWriteOnARecordAnEarlierChangedWriteTouchedFoldsIntoThatWrite(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	const add = "shop.catalog.v1.StockService/AddStock"
	rec := shopRecord(
		shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`).failing("product.created_at", "t", nil),
		shopStep("add_negative", add, `{"qty_on_hand":"0",`+shopOK+`}`, "create").failing("status.code", "REJECTED", "SUCCESS"),
		shopStep("add_large", add, `{"qty_on_hand":"1250",`+shopOK+`}`, "create").failing("qty_on_hand", "1251", "1250"),
		shopStep("add_larger", add, `{"qty_on_hand":"13595",`+shopOK+`}`, "create").failing("qty_on_hand", "13596", "13595"),
		shopStep("create_other", shopCreate, `{"product":{"id_product":"p2"}}`),
		shopStep("add_other", add, `{"qty_on_hand":"3",`+shopOK+`}`, "create_other").failing("qty_on_hand", "4", "3"),
	)
	for _, step := range []string{"add_large", "add_larger"} {
		if write, own, _ := blameOf(t, rec, step, "qty_on_hand"); write != "add_negative" || own != "" {
			t.Errorf("%s: got write %q own %q, want add_negative", step, write, own)
		}
	}
	if write, _, _ := blameOf(t, rec, "add_other", "qty_on_hand"); write != "" {
		t.Errorf("a write on another record stays its own, got write %q", write)
	}
	a := runAttribution(nil, rec)
	g := &gateChain{name: "c", failed: true}
	for _, st := range []string{"add_negative", "add_large", "add_larger"} {
		path := "qty_on_hand"
		if st == "add_negative" {
			path = "status.code"
		}
		g.items = append(g.items, a.item(gateItem{Step: st, Call: add, Path: path, Failed: true}))
	}
	out := captureStdout(t, func() { printGateGroups([]*gateChain{g}, false) })
	if strings.Count(out, "\n  ") != 1 || !strings.Contains(out, "StockService/AddStock: 3 step(s)") {
		t.Errorf("knock-on changes on the same record fold into the root write:\n%s", out)
	}
}

func TestALaterWriteChangingAnotherFieldOfTheRecordKeepsItsOwnBlame(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	rec := shopRecord(
		shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","total_minor":"7"},`+shopOK+`}`).failing("order.total_minor", "9", "7"),
		shopStep("cancel", shopCancel, `{"order":{"id_order":"o1","total_minor":"7","status":"ORDER_STATUS_CANCELLED"},`+shopOK+`}`, "create_order").failing("order.total_minor", "9", "7"),
		shopStep("replay", shopOrder, `{"order":{"id_order":"o1","total_minor":"7","status":"ORDER_STATUS_PENDING"},`+shopOK+`}`, "create_order").failing("order.status", "ORDER_STATUS_CANCELLED", "ORDER_STATUS_PENDING").failing("order.total_minor", "9", "7"),
	)
	if write, _, _ := blameOf(t, rec, "replay", "order.status"); write == "cancel" {
		t.Errorf("a status change is not a knock-on of an earlier write whose total changed, got write %q", write)
	}
}
