package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func TestQualityChargesAnRPCNoOverlayCovers(t *testing.T) {
	cat := catalogtest.Shop()
	unary := []string{}
	for _, m := range cat.Methods() {
		if !m.Streaming() {
			unary = append(unary, m.FullName)
		}
	}
	if len(unary) < 2 {
		t.Fatal("the shop fixture carries at least two unary rpcs")
	}
	covered := map[string]*contract.RPCContract{}
	for _, name := range unary {
		covered[name] = &contract.RPCContract{}
	}
	full := contract.Measure(contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/contract/v1", Domain: "all", RPCs: covered,
	}}), cat, "")

	dropped := unary[0]
	delete(covered, dropped)
	partial := contract.Measure(contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/contract/v1", Domain: "all", RPCs: covered,
	}}), cat, "")

	var row *contract.QualityRPC
	for i := range partial.RPCs {
		if partial.RPCs[i].RPC == dropped {
			row = &partial.RPCs[i]
		}
	}
	if row == nil || !row.NoContract {
		t.Fatalf("%s has no contract in any overlay and was not charged: rows %+v", dropped, partial.RPCs)
	}
	if partial.TotalScore <= full.TotalScore {
		t.Fatalf("deleting the contract for %s moved the score from %d to %d: a gate you can satisfy by "+
			"deleting the file is measuring the wrong thing", dropped, full.TotalScore, partial.TotalScore)
	}

	none := contract.Measure(contract.NewLibrary(nil), cat, "")
	if none.TotalScore <= full.TotalScore {
		t.Fatalf("an empty contracts directory scored %d, no worse than bare entries for every rpc at %d",
			none.TotalScore, full.TotalScore)
	}
}
