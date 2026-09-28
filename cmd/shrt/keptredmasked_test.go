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

func TestAKeptRedPinThatMovedLeadsAndAGonePinKeepsTheRunsLine(t *testing.T) {
	gone := "run: kept red, but it passed: the pinned defect is gone"
	chains := []*gateChain{
		{name: "confirm-slice", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{
			{Step: "confirm_short", Call: maskConfirm, Path: "order.status", Want: "PENDING", Got: "REJECTED", Pinned: "CONFIRMED"}}},
		{name: "list-slice", failed: true, keptRed: runner.KeptRedGone, first: gone, items: []gateItem{
			{Step: "list_pending", Call: maskList, Path: "orders.1", Rule: "exists", Want: "false", Got: "false", Pinned: "true", Passes: true}}},
		{name: "fixed-slice", failed: true, keptRed: runner.KeptRedGone, first: gone, items: []gateItem{
			{Step: "get", Call: maskGet, Path: "product.qty_on_hand", Want: "4", Got: "4", Pinned: "-1", Passes: true}}},
	}
	settleGate(chains)
	for i, want := range map[int]string{
		0: "confirm_short (OrderService/ConfirmOrder) order.status pinned got=CONFIRMED, now got=REJECTED",
		1: gone,
		2: gone,
	} {
		if chains[i].first != want {
			t.Errorf("%s: got %q, want %q", chains[i].name, chains[i].first, want)
		}
	}
	if chains[0].class != "not as pinned" {
		t.Errorf("got class %q", chains[0].class)
	}
	out := captureStdout(t, func() { printGateGroups(chains, false) })
	if strings.Contains(out, "GetProduct") || strings.Contains(out, "ListOrders") {
		t.Errorf("a pin that passes is no failure to group:\n%s", out)
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
	if b := pinnedAttribution(nil, rec, held).of("list_cancelled", "orders"); !b.blames() {
		t.Fatalf("judged by its expectations alone, the read changed after a write: %+v", b)
	}
	drift := []diff.Change{{Step: "list_cancelled", Path: "orders", Kind: diff.KindLength, Want: 3, Got: 2}}
	b := changesAttribution(nil, rec, drift).of("list_cancelled", "orders")
	if b.Kind != reasonSet || b.Path != "orders" {
		t.Errorf("against the run it drifted from, the writes answered as before, so the read is the suspect: %+v", b)
	}
}

func TestAGateGroupCountsLaterStepsOfItsOwnRpcAsItsOwn(t *testing.T) {
	const create = "shop.orders.v1.OrderService/CreateOrder"
	chains := []*gateChain{{name: "orders", failed: true, items: []gateItem{
		{Step: "create", Call: create, Path: "order.lines", Want: "a", Got: "b"},
		{Step: "create_2", Call: create, Path: "order.lines", Want: "a", Got: "b", Reason: reason{Kind: reasonWrite, Step: "create", RPC: create}},
	}}}
	out := captureStdout(t, func() { printGateGroups(chains, false) })
	if !strings.Contains(out, "OrderService/CreateOrder: 2 step(s) in 1 chain(s); e.g. orders create_2; suspect write create") || strings.Count(out, "\n  ") != 1 {
		t.Errorf("a later CreateOrder failing on the same path is another occurrence, not a step after it:\n%s", out)
	}
}

func TestAGateGroupExampleIsARealChangeNotAMaskedPinThatNowPasses(t *testing.T) {
	chains := []*gateChain{
		{name: "stock-slice", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{
			{Step: "get_2", Call: maskGet, Path: "product.qty_on_hand", Want: "1", Got: "1", Pinned: "-1", Passes: true}}},
		{name: "lifecycle", failed: true, items: []gateItem{
			{Step: "get", Call: maskGet, Path: "product.qty_on_hand", Want: "5", Got: "4", Reason: reason{Kind: reasonSet, Step: "get", RPC: maskGet, Path: "product"}}}},
	}
	settleGate(chains)
	out := captureStdout(t, func() { printGateGroups(chains, false) })
	if !strings.Contains(out, "e.g. lifecycle get; suspect read get") {
		t.Errorf("the example is the step that changed, not the pin that now passes:\n%s", out)
	}
	if got := chains[0].items[0].wantGot(); got != "pinned got=-1, now got=1" {
		t.Errorf("a pin reads as pinned and now: %q", got)
	}
}
