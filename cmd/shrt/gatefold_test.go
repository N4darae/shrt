package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestAHedgedReadNamesTheWriteThatHeadsTheHedge(t *testing.T) {
	const add = "shop.catalog.v1.StockService/AddStock"
	order := `{"order":{"id_order":"o1","status":"CONFIRMED","lines":[{"id_product":"p1","qty":"2"}]}}`
	rec := shopRecord(
		shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","qty_on_hand":"0"}}`),
		shopStep("add_stock", add, `{"qty_on_hand":"10"}`, "create_product"),
		shopStep("create_order", shopOrder, order, "create_product"),
		shopStep("confirm_order", shopConfirm, order, "create_order"),
		shopStep("get", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"9"}}`, "create_product"),
	)
	moved := []diff.Change{{Step: "get", Path: "product.qty_on_hand", Kind: diff.KindChanged, Want: "8", Got: "9"}}
	it := changesAttribution(effectsEnv(t), rec, moved).item(gateItem{Step: "get", Call: shopGet, Path: "product.qty_on_hand"})
	if it.Suspect != shopConfirm || it.SuspectStep != "confirm_order" || it.Firm || !strings.HasPrefix(it.Why, eitherWhy+"confirm_order (ConfirmOrder)") {
		t.Errorf("the hedge is headed by confirm_order, so the gate files the read under ConfirmOrder: %+v", it)
	}
}

func TestAReadAttributedToAWriteIsGroupedAndPinnedUnderThatWrite(t *testing.T) {
	const qty = "product.qty_on_hand"
	firm := "its contract moves qty_on_hand; the other writes on that record answered as before"
	chains := []*gateChain{
		{name: "lifecycle", failed: true, items: []gateItem{{Step: "stock_after_confirm", Call: maskGet, Path: qty, Want: "5", Got: "8",
			Suspect: maskConfirm, SuspectStep: "confirm_order", Why: firm, Firm: true, Failed: true}}},
		{name: "lifecycle-slice", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{{Step: "stock_after_cancel", Call: maskGet, Path: qty,
			Want: "10", Got: "8", Pinned: "5", Suspect: shopCancel, SuspectStep: "cancel_order"}}},
		{name: "twice", failed: true, items: []gateItem{{Step: "get_after_twice", Call: maskGet, Path: qty, Want: "7", Got: "8",
			Suspect: maskConfirm, SuspectStep: "confirm_twice", Why: eitherWhy + "confirm_twice (ConfirmOrder), or earlier add_stock, answered as before", Failed: true}}},
	}
	settleGate(chains)
	headlineGate(chains)
	if want := "stock_after_cancel product.qty_on_hand pinned got=5, now got=8; moved with ConfirmOrder product.qty_on_hand, reported above"; chains[1].first != want {
		t.Errorf("got %q, want %q", chains[1].first, want)
	}
	out := captureStdout(t, func() { printGateGroups(chains) })
	if strings.Contains(out, "ProductService/GetProduct:") || !strings.Contains(out, "OrderService/ConfirmOrder: suspect the write: "+firm) ||
		!strings.Contains(out, "+3 step(s) after it: GetProduct product.qty_on_hand") {
		t.Errorf("each read moved by ConfirmOrder is a step after ConfirmOrder, not a GetProduct suspect:\n%s", out)
	}
}
