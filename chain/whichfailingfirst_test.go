package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestUnderRPCTheChainWhoseNewestRunFailsThereRanksFirst(t *testing.T) {
	ok := map[string]any{"error": map[string]any{"code": "OK"}}
	hits := chain.Which(whichFixture(), chain.WhichQuery{RPC: "pkg.Svc/Create"}, chain.WhichOptions{
		Observations: func(name string) []chain.Observation {
			switch name {
			case "short":
				return []chain.Observation{{Run: "r1", Step: "seed", Status: "passed", Reached: true, Response: ok}}
			case "long":
				return []chain.Observation{
					{Run: "r2", Step: "a", Status: "passed", Reached: true, Response: ok},
					{Run: "r2", Step: "b", Status: "failed", Reached: true, Response: ok,
						Failures: []chain.ExpectResult{{Path: "total", Rule: "equals", Want: 4548, Got: 6250}}},
				}
			}
			return nil
		},
	})
	if len(hits) != 2 {
		t.Fatalf("both chains call Create, got %d", len(hits))
	}
	if hits[0].Chain != "long" || hits[0].Best != "b" {
		t.Fatalf("during an incident the step whose newest run FAILED at the rpc is the one to slice; got %s %s first", hits[0].Chain, hits[0].Best)
	}
	if want := "shrt chain slice long -step b -mode pin -run r2"; hits[0].Command != want {
		t.Fatalf("the first reproduce line must slice the failing step\nwant %q\ngot  %q", want, hits[0].Command)
	}
}
