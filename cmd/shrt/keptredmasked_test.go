package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

const (
	maskList    = "shop.orders.v1.OrderService/ListOrders"
	maskConfirm = "shop.orders.v1.OrderService/ConfirmOrder"
	maskGet     = "shop.catalog.v1.ProductService/GetProduct"
)

func TestAKeptRedPinThatPassesOrMovesWhereTheGateReportsItsRpcNamesThatRpc(t *testing.T) {
	chains := []*gateChain{
		{name: "confirm-slice", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{
			{Step: "confirm_short", Call: maskConfirm, Path: "order.status", Want: "PENDING", Got: "REJECTED", Pinned: "CONFIRMED"}}},
		{name: "confirm-flow", failed: true, items: []gateItem{{Step: "confirm", Call: maskConfirm, Path: "order.status", Want: "CONFIRMED", Got: "PENDING"}}},
		{name: "list-flow", failed: true, items: []gateItem{{Step: "list_after", Call: maskList, Path: "orders", Want: "2", Got: "1",
			Own: "ListOrders answers another set of orders"}}},
		{name: "list-slice", failed: true, keptRed: runner.KeptRedGone, first: "run: kept red, but it passed: the pinned defect is gone", items: []gateItem{
			{Step: "list_pending", Call: maskList, Path: "orders.1", Rule: "exists", Want: "false", Got: "false", Pinned: "true", Passes: true,
				Suspect: maskConfirm, SuspectStep: "confirm_ok"}}},
		{name: "fixed-slice", failed: true, keptRed: runner.KeptRedGone, first: "run: kept red, but it passed: the pinned defect is gone", items: []gateItem{
			{Step: "get", Call: maskGet, Path: "product.qty_on_hand", Want: "4", Got: "4", Pinned: "-1", Passes: true}}},
	}
	settleGate(chains)
	headlineGate(chains)
	for i, want := range map[int]string{
		0: "confirm_short order.status pinned got=CONFIRMED, now got=REJECTED; moved with ConfirmOrder order.status, reported below",
		3: "list_pending orders.1 pinned got=true, now got=false; masked by ListOrders orders, reported above",
		4: "run: kept red, but it passed: the pinned defect is gone",
	} {
		if chains[i].first != want {
			t.Errorf("%s: got %q, want %q", chains[i].name, chains[i].first, want)
		}
	}
	if chains[3].items[0].Suspect != "" {
		t.Errorf("the masked read is the rpc the gate proved, not the write before it: %+v", chains[3].items[0])
	}
	out := captureStdout(t, func() { printGateGroups(chains) })
	if strings.Contains(out, "GetProduct") || !strings.Contains(out, "OrderService/ListOrders: 2 step(s) in 2 chain(s)") {
		t.Errorf("a pin that passes with nothing else changed there is no failure to group; a masked one is:\n%s", out)
	}
}

func TestAKeptRedDriftOnAReadTheGateProvedIsThatReadNotTheWriteBeforeIt(t *testing.T) {
	chains := []*gateChain{
		{name: "list-flow", failed: true, firstAt: "list_after orders", items: []gateItem{{Step: "list_after", Call: maskList, Path: "orders", Want: "2", Got: "1",
			Own: "ListOrders answers another set of orders"}}},
		{name: "list-slice", failed: true, keptRed: runner.KeptRedNotAsPinned, pinsHeld: true, items: []gateItem{
			{Step: "list_cancelled", Call: maskList, Path: "orders", Want: "3", Got: "2", Suspect: maskConfirm, SuspectStep: "confirm_3"}}},
	}
	settleGate(chains)
	headlineGate(chains)
	if want := "pins held, new change: 1 step(s) from ListOrders orders, reported above"; chains[1].line(0) != "FAIL       list-slice  "+want {
		t.Errorf("got %q, want %q", chains[1].line(0), want)
	}
	if out := captureStdout(t, func() { printGateGroups(chains) }); strings.Contains(out, "ConfirmOrder") {
		t.Errorf("the write before the proven read is no suspect:\n%s", out)
	}
}

func TestAKeptRedDriftIsJudgedAgainstTheRunItDriftedFrom(t *testing.T) {
	create := shopStep("create_customer", "shop.customers.v1.CustomerService/CreateCustomer", `{"customer":{"id_customer":"c1"}}`)
	confirm := shopStep("confirm_3", maskConfirm, `{"order":{"id_order":"o3"}}`, "create_customer")
	list := shopStep("list_cancelled", maskList, `{"orders":[{"id_order":"o1"},{"id_order":"o2"}]}`, "create_customer")
	list.Status = runner.StatusFailed
	list.Expect = []chain.ExpectResult{{Path: "orders.1", Rule: "exists", Want: false, Got: true}}
	rec := shopRecord(create, confirm, list)
	held := map[string]bool{"list_cancelled orders.1": true}
	if b := pinnedAttribution(nil, rec, held).of("list_cancelled", "orders"); b.write < 0 {
		t.Fatalf("judged by its expectations alone, the read changed after a write: %+v", b)
	}
	drift := []diff.Change{{Step: "list_cancelled", Path: "orders", Kind: diff.KindLength, Want: 3, Got: 2}}
	b := changesAttribution(nil, rec, drift).of("list_cancelled", "orders")
	if b.write >= 0 || !strings.Contains(b.own, "the writes before it answered as before") {
		t.Errorf("against the run it drifted from, the writes answered as before, so the read is the suspect: %+v", b)
	}
}

func TestAGateGroupCountsLaterStepsOfItsOwnRpcAsItsOwn(t *testing.T) {
	const create = "shop.orders.v1.OrderService/CreateOrder"
	chains := []*gateChain{{name: "orders", failed: true, items: []gateItem{
		{Step: "create", Call: create, Path: "order.lines", Want: "a", Got: "b"},
		{Step: "create_2", Call: create, Path: "order.lines", Want: "a", Got: "b", Suspect: create, SuspectStep: "create"},
	}}}
	out := captureStdout(t, func() { printGateGroups(chains) })
	if !strings.Contains(out, "OrderService/CreateOrder: 2 step(s) in 1 chain(s), paths order.lines; e.g. orders create order.lines") || strings.Contains(out, "after it") {
		t.Errorf("a later CreateOrder failing on the same path is another occurrence, not a step after it:\n%s", out)
	}
}

func TestAGateGroupExampleIsARealChangeNotAMaskedPinThatNowPasses(t *testing.T) {
	chains := []*gateChain{
		{name: "stock-slice", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{
			{Step: "get_2", Call: maskGet, Path: "product.qty_on_hand", Want: "1", Got: "1", Pinned: "-1", Passes: true}}},
		{name: "lifecycle", failed: true, items: []gateItem{
			{Step: "get", Call: maskGet, Path: "product.qty_on_hand", Want: "5", Got: "4", Own: "GetProduct reads qty_on_hand unlike the writes"}}},
	}
	settleGate(chains)
	out := captureStdout(t, func() { printGateGroups(chains) })
	if !strings.Contains(out, "e.g. lifecycle get product.qty_on_hand want=5 got=4") {
		t.Errorf("the example is the step that changed, not the pin that now passes:\n%s", out)
	}
	if got := chains[0].items[0].wantGot(); got != "pinned got=-1, now got=1" {
		t.Errorf("a pin reads as pinned and now: %q", got)
	}
}
