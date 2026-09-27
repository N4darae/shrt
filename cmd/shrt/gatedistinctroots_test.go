package main

import (
	"strings"
	"testing"
)

func TestAGateGroupLeadsWithItsLargestChangeAndShowsEachOtherRootChange(t *testing.T) {
	const create, fetch = "x.v1.OrderService/CreateOrder", "x.v1.OrderService/FetchOrder"
	total := func(step string) gateItem {
		return gateItem{Step: step, Call: create, Path: "order.total_minor", Want: "600", Got: "400", Failed: true}
	}
	read := func(step, root string) gateItem {
		return gateItem{Step: step, Call: fetch, Path: "order.total_minor", Want: "600", Got: "400", Suspect: create, SuspectStep: root}
	}
	chains := []*gateChain{
		{name: "replay", failed: true, firstAt: "replay order.status", items: []gateItem{
			{Step: "replay", Call: create, Path: "order.status", Want: "CONFIRMED", Got: "PENDING", Failed: true}}},
		{name: "orders", failed: true, items: []gateItem{total("create"), read("fetch", "create"), total("create_2"), read("fetch_2", "create_2"),
			{Step: "replay_2", Call: create, Path: "order.status", Want: "CANCELLED", Got: "PENDING", Failed: true},
			{Step: "replay_2", Call: create, Path: "order.total_minor", Want: "600", Got: "400", Suspect: create, SuspectStep: "create"}}},
		{name: "lists", failed: true, items: []gateItem{total("create"), read("fetch", "create")}},
	}
	settleGate(chains)
	headlineGate(chains)
	if want := "5 step(s) from CreateOrder total_minor and status, reported above"; chains[1].first != want {
		t.Errorf("got %q, want %q", chains[1].first, want)
	}
	if want := "2 step(s) from CreateOrder total_minor, reported above"; chains[2].first != want {
		t.Errorf("got %q, want %q", chains[2].first, want)
	}
	out := captureStdout(t, func() { printGateGroups(chains) })
	for _, want := range []string{
		"  OrderService/CreateOrder: 5 step(s) in 3 chain(s), paths order.total_minor, order.status; e.g. orders create order.total_minor want=600 got=400\n",
		"    another change: 2 step(s) in 2 chain(s); e.g. replay replay order.status want=CONFIRMED got=PENDING\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

func TestAGateGroupSplitsOneRefusalPathByTheProfileOfItsRootSteps(t *testing.T) {
	const add, get = "x.v1.StockService/AddStock", "x.v1.ProductService/GetProduct"
	accepted := func(step, variant string) []gateItem {
		return []gateItem{
			{Step: step, Call: add, Path: "error.code", Rule: "not_equal", Want: "SUCCESS", Got: "SUCCESS", Variant: variant, Failed: true},
			{Step: step, Call: add, Path: "qty_on_hand", Want: "0", Got: "10", Variant: variant},
		}
	}
	chains := []*gateChain{
		{name: "catalog", failed: true, items: append(accepted("add_zero", ""), accepted("add_as_clerk", "as clerk")...)},
		{name: "roles", failed: true, items: append(accepted("clerk_add", "as clerk"),
			gateItem{Step: "clerk_get", Call: get, Path: "product.qty_on_hand", Want: "6", Got: "15", Suspect: add, SuspectStep: "clerk_add", Variant: "as clerk"})},
	}
	mergeProfiles(chains)
	out := captureStdout(t, func() { printGateGroups(chains) })
	for _, want := range []string{
		"  StockService/AddStock: 3 step(s) in 2 chain(s), paths error.code, qty_on_hand; e.g. catalog add_as_clerk as clerk error.code want≠SUCCESS got=SUCCESS\n",
		"    another change: 1 step(s) in 1 chain(s); e.g. catalog add_zero error.code want≠SUCCESS got=SUCCESS\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}
