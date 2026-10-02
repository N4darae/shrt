package main

import (
	"strings"
	"testing"
)

func TestTheGateHeadlinesAChainByAFaultNoEarlierChainShowed(t *testing.T) {
	const create, fetch = "shrt.test.v1.ThingService/Create", "shrt.test.v1.ThingService/Fetch"
	price := gateItem{Step: "create", Call: create, Path: "thing.price", Want: "250", Got: "249", Reason: reason{Kind: reasonWrite, Step: "create", RPC: create}}
	after := gateItem{Step: "get", Call: fetch, Path: "thing.price", Want: "250", Got: "249", Reason: reason{Kind: reasonWrite, Step: "create", RPC: create}}
	refused := gateItem{Step: "fetch_as_other", Call: fetch, Path: "error.code", Want: "OK", Got: "DENIED", Kind: "refused",
		Reason: reason{Kind: reasonRefused, Step: "fetch_as_other", RPC: fetch, Profile: "other", Got: "DENIED", Other: "default"}}
	gateWorkspace(t, map[string][]gateOutcome{
		"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: []gateItem{price, after, refused}}}},
		"run cli-unique":        {{code: 1, side: gateSidecar{KeptRed: "not_as_pinned", PinsHeld: true, Items: []gateItem{price, after}}}},
	})
	out, code := runGateOut(t)
	for _, want := range []string{
		"FAIL       cli-thing-flow  create (ThingService/Create) thing.price want=250 got=249; suspect write create (ThingService/Create); also suspect read fetch_as_other (ThingService/Fetch) as other: refused (DENIED), passes as default\n",
		"FAIL       cli-unique      pins held, new change: create (ThingService/Create) thing.price want=250 got=249; same fault as cli-thing-flow (Create)\n",
		"  ThingService/Create: 4 step(s) in 2 chain(s); e.g. cli-thing-flow create; suspect write create (ThingService/Create)\n",
		"  ThingService/Fetch: 1 step(s) in 1 chain(s); e.g. cli-thing-flow fetch_as_other; suspect read fetch_as_other (ThingService/Fetch) as other: refused (DENIED), passes as default\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if code != 1 {
		t.Errorf("a drifted kept-red chain still fails the gate, got exit %d", code)
	}
}

func TestAKeptRedChainWhosePinMovedNamesThePinAndItsSuspect(t *testing.T) {
	const create, move, get = "shrt.test.v1.ThingService/Create", "shrt.test.v1.ThingService/Move", "shrt.test.v1.ThingService/Get"
	blamed := reason{Kind: reasonWrite, Step: "move", RPC: move}
	after := gateItem{Step: "get", Call: get, Path: "thing.level", Want: "10", Got: "-1", Reason: blamed}
	pin := gateItem{Step: "get_pinned", Call: get, Path: "thing.level", Want: "-1", Got: "-3", Pinned: "8", Reason: blamed}
	drift := gateItem{Step: "create", Call: create, Path: "thing.price", Want: "250", Got: "249"}
	gateWorkspace(t, map[string][]gateOutcome{
		"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: []gateItem{after, drift}}}},
		"run cli-unique":        {{code: 1, side: gateSidecar{KeptRed: "not_as_pinned", Items: []gateItem{drift, pin}}}},
	})
	out, _ := runGateOut(t)
	want := "FAIL       cli-unique      not as pinned: get_pinned (ThingService/Get) thing.level pinned got=8, now got=-3; same fault as cli-thing-flow (Move)\n"
	if !strings.Contains(out, want) {
		t.Errorf("want %q in:\n%s", want, out)
	}
}

func TestAMovedPinWithNoSuspectDoesNotPointAbove(t *testing.T) {
	const get = "shrt.test.v1.ThingService/Get"
	chains := []*gateChain{
		{name: "a", failed: true, items: []gateItem{{Step: "get", Call: get, Path: "thing.level", Want: "5", Got: "8"}}},
		{name: "b", failed: true, items: []gateItem{{Step: "get_pinned", Call: get, Path: "thing.level", Want: "4", Got: "2", Pinned: "-1"}}},
	}
	settleGate(chains)
	if want := "get_pinned (ThingService/Get) thing.level pinned got=-1, now got=2"; chains[1].first != want || chains[1].class != "not as pinned" {
		t.Errorf("got %q %q, want %q", chains[1].class, chains[1].first, want)
	}
}

func TestAKeptRedChainWhosePinsHeldNamesANewChangeWithoutARePin(t *testing.T) {
	const fetch = "shrt.test.v1.ThingService/Fetch"
	drift := gateItem{Step: "fetch", Call: fetch, Path: "thing.total", Want: "9", Got: "0"}
	gateWorkspace(t, map[string][]gateOutcome{
		"run cli-unique": {{code: 1, side: gateSidecar{Error: "chain cli-unique: kept red, but it did not fail as pinned: a pinned step now returns something else", KeptRed: "not_as_pinned", PinsHeld: true, Items: []gateItem{drift}}}},
	})
	out, _ := runGateOut(t)
	if want := "FAIL       cli-unique      pins held, new change: fetch (ThingService/Fetch) thing.total want=9 got=0\n"; !strings.Contains(out, want) || strings.Contains(out, "-force") {
		t.Errorf("want %q and no re-pin in:\n%s", want, out)
	}
}

func TestAFailedFirstChangeShownAboveStillLeadsOverADrift(t *testing.T) {
	const add, create = "shrt.test.v1.ThingService/Add", "shrt.test.v1.ThingService/Create"
	failed := func(step string) gateItem {
		return gateItem{Step: step, Call: add, Path: "status.code", Want: "REJECTED", Got: "SUCCESS", Failed: true, Reason: reason{Kind: reasonWrite, Step: step, RPC: add}}
	}
	drift := gateItem{Step: "create", Call: create, Path: "thing.name", Want: "long name", Got: "long"}
	chains := []*gateChain{
		{name: "a", failed: true, items: []gateItem{failed("add")}},
		{name: "b", failed: true, items: []gateItem{failed("add_as_clerk"), failed("add_again"), drift}},
	}
	settleGate(chains)
	for i, want := range []string{
		"add (ThingService/Add) status.code want=REJECTED got=SUCCESS; suspect write add (ThingService/Add)",
		"add_as_clerk (ThingService/Add) status.code want=REJECTED got=SUCCESS; same fault as a (Add)",
	} {
		if chains[i].first != want {
			t.Errorf("chain %s: got %q, want %q", chains[i].name, chains[i].first, want)
		}
	}
}

func TestTheGateTellsTheWriteFromTheReadOnlyUnderV(t *testing.T) {
	const create, fetch = "shrt.test.v1.ThingService/Create", "shrt.test.v1.ThingService/Fetch"
	it := gateItem{Step: "fetch", Call: fetch, Path: "name", Want: `"a "`, Got: "a", Failed: true,
		Reason: reason{Kind: reasonUnclear, Step: "create", RPC: create, Read: "fetch", ReadRPC: fetch, Path: "name", Want: "a ", Got: "a"}}
	gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: []gateItem{it}}}}})
	const hint = "\n  tell them apart: read name through PartnerService/FetchMine (name)\n"
	if out, _ := runGateOut(t); strings.Contains(out, "tell them apart") {
		t.Errorf("the default gate row carries no hint:\n%s", out)
	}
	if out, _ := runGateOut(t, "-v"); !strings.Contains(out, hint) {
		t.Errorf("want %q under -v in:\n%s", hint, out)
	}
}
