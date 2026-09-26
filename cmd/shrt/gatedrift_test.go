package main

import (
	"strings"
	"testing"
)

func TestTheGateHeadlinesAChainByItsFirstChangeNotReportedAbove(t *testing.T) {
	const create, fetch = "shrt.test.v1.ThingService/Create", "shrt.test.v1.ThingService/Fetch"
	price := func(step string) gateItem {
		return gateItem{Step: step, Call: create, Path: "thing.price", Want: "250", Got: "249"}
	}
	after := gateItem{Step: "get", Call: fetch, Path: "thing.price", Want: "250", Got: "249", Suspect: create, SuspectStep: "create"}
	refused := gateItem{Step: "fetch_as_other", Call: fetch, Path: "error.code", Want: "OK", Got: "DENIED", Kind: "refused",
		Own: "Fetch passes as default, refused as other (DENIED)"}
	gateWorkspace(t, map[string][]gateOutcome{
		"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: []gateItem{price("create"), after, refused}}}},
		"run cli-unique":        {{code: 1, side: gateSidecar{KeptRed: "not_as_pinned", PinsHeld: true, Items: []gateItem{price("create"), after}}}},
	})
	out, code := runGateOut(t, "-v")
	for _, want := range []string{
		"FAIL       cli-thing-flow  create (ThingService/Create) thing.price want=250 got=249\n",
		"FAIL       cli-unique      kept red, drifted: pinned defect unchanged; setup drift from Create price (reported above)\n",
		"distinct changes (suspect rpc, path, kind):\n" +
			"  ThingService/Create thing.price value: 2 step(s) in 2 chain(s); e.g. cli-thing-flow create thing.price want=250 got=249\n" +
			"  ThingService/Fetch error.code refused: 1 step(s) in 1 chain(s); e.g. cli-thing-flow fetch_as_other error.code want=OK got=DENIED\n",
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
	after := gateItem{Step: "get", Call: get, Path: "thing.level", Want: "10", Got: "-1", Suspect: move, SuspectStep: "move", Variant: "refused (1305 TooFew)"}
	pin := gateItem{Step: "get_pinned", Call: get, Path: "thing.level", Want: "-1", Got: "-3", Pinned: "8", Suspect: move, SuspectStep: "move", Variant: "refused (1305 TooFew)"}
	drift := gateItem{Step: "create", Call: create, Path: "thing.price", Want: "250", Got: "249"}
	gateWorkspace(t, map[string][]gateOutcome{
		"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: []gateItem{after, drift}}}},
		"run cli-unique":        {{code: 1, side: gateSidecar{KeptRed: "not_as_pinned", Items: []gateItem{drift, pin}}}},
	})
	out, _ := runGateOut(t)
	want := "FAIL       cli-unique      not as pinned: get_pinned thing.level pinned got=8, now got=-3; suspect Move refused (1305 TooFew), reported above\n"
	if !strings.Contains(out, want) {
		t.Errorf("want %q in:\n%s", want, out)
	}
}

func TestAMovedPinAfterAnotherWriteThanTheOneChangingThatReadElsewhereNamesBoth(t *testing.T) {
	const confirm, cancel, get = "shrt.test.v1.ThingService/Confirm", "shrt.test.v1.ThingService/Cancel", "shrt.test.v1.ThingService/Get"
	chains := []*gateChain{
		{name: "a", failed: true, items: []gateItem{{Step: "get", Call: get, Path: "thing.level", Want: "3", Got: "2", Suspect: confirm, SuspectStep: "confirm"}}},
		{name: "b", failed: true, class: "not as pinned", items: []gateItem{{Step: "get_pinned", Call: get, Path: "thing.level", Want: "6", Got: "2", Pinned: "3", Suspect: cancel, SuspectStep: "cancel"}}},
	}
	settleGate(chains)
	headlineGate(chains)
	if want := "get_pinned thing.level pinned got=3, now got=2; suspect Cancel, or Confirm as at other steps reading Get level"; chains[1].first != want {
		t.Errorf("got %q, want %q", chains[1].first, want)
	}
	if chains[0].items[0].Own != "" {
		t.Errorf("a moved pin is not a second write the read changes after: %+v", chains[0].items[0])
	}
	out := captureStdout(t, func() { printDistinct(chains) })
	if strings.Contains(out, "Cancel") {
		t.Errorf("an undecided pin gets no row of its own:\n%s", out)
	}
}

func TestAKeptRedChainWhosePinsHeldNamesANewChangeWithoutARePin(t *testing.T) {
	const fetch = "shrt.test.v1.ThingService/Fetch"
	drift := gateItem{Step: "fetch", Call: fetch, Path: "thing.total", Want: "9", Got: "0"}
	gateWorkspace(t, map[string][]gateOutcome{
		"run cli-unique": {{code: 1, stderr: "shrt run: chain cli-unique: kept red, but it did not fail as pinned: a pinned step now returns something else",
			side: gateSidecar{KeptRed: "not_as_pinned", PinsHeld: true, Items: []gateItem{drift}}}},
	})
	out, _ := runGateOut(t)
	if want := "FAIL       cli-unique      pins held, new change: fetch (ThingService/Fetch) thing.total want=9 got=0\n"; !strings.Contains(out, want) || strings.Contains(out, "-force") {
		t.Errorf("want %q and no re-pin in:\n%s", want, out)
	}
}
