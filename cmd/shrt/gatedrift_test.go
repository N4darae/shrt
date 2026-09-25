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
		"run cli-thing-flow":    {{code: 1, side: gateSidecar{Items: []gateItem{price("create"), after}}}},
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
