package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestWhichNamesTheStepTheNewestRunStoppedAt(t *testing.T) {
	hits := chain.Which(whichFixture(), chain.WhichQuery{Code: "1218"}, chain.WhichOptions{
		Observations: func(name string) []chain.Observation {
			if name != "long" {
				return nil
			}
			return []chain.Observation{
				{Run: "r3", Step: "a", Status: "failed", Reached: true, Response: okResponse()},
			}
		},
	})
	_, m := matchOf(t, hits, "long", "boom_long")
	if m.Newest == nil || m.Newest.StoppedAt != "a" || m.Newest.StoppedStatus != "failed" {
		t.Fatalf("the newest run stopped at step a, which failed, and the match must say so: %+v", m.Newest)
	}
}
