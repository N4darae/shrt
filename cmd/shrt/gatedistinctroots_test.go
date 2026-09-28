package main

import "testing"

func TestAChainHeadlinesAFaultNoEarlierChainShowedOrNamesTheChainThatDid(t *testing.T) {
	const create = "x.v1.OrderService/CreateOrder"
	status := func(step string) gateItem {
		return gateItem{Step: step, Call: create, Path: "order.status", Want: "CONFIRMED", Got: "PENDING", Failed: true, Reason: reason{Kind: reasonWrite, Step: step, RPC: create}}
	}
	total := gateItem{Step: "create", Call: create, Path: "order.total_minor", Want: "600", Got: "400", Failed: true, Reason: reason{Kind: reasonWrite, Step: "create", RPC: create}}
	chains := []*gateChain{
		{name: "replay", failed: true, items: []gateItem{status("replay")}},
		{name: "orders", failed: true, items: []gateItem{status("replay_2"), total}},
		{name: "lists", failed: true, items: []gateItem{total}},
	}
	settleGate(chains)
	for i, want := range []string{
		"replay (OrderService/CreateOrder) order.status want=CONFIRMED got=PENDING; suspect write replay (OrderService/CreateOrder)",
		"create (OrderService/CreateOrder) order.total_minor want=600 got=400; suspect write create (OrderService/CreateOrder)",
		"create (OrderService/CreateOrder) order.total_minor want=600 got=400; same fault as orders",
	} {
		if chains[i].first != want {
			t.Errorf("%s: got %q, want %q", chains[i].name, chains[i].first, want)
		}
	}
}

func TestAReadsOwnFaultIsOneRootWhateverPathShowsIt(t *testing.T) {
	const get, create = "x.v1.S/Get", "x.v1.S/Create"
	refused := reason{Kind: reasonRefused, Step: "get", RPC: get, Got: "1102"}
	a := gateItem{Step: "get", Call: get, Path: "status.code", Reason: refused}
	b := gateItem{Step: "get", Call: get, Path: "customer.name", Reason: refused}
	w := gateItem{Step: "get", Call: get, Path: "customer.name", Reason: reason{Kind: reasonWrite, Step: "create", RPC: create}}
	if a.root() != b.root() || a.root() == w.root() || w.root() != "S/Create name" {
		t.Errorf("got %q, %q, %q", a.root(), b.root(), w.root())
	}
}
