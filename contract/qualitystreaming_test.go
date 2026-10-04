package contract_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func TestQualityDoesNotChargeAStreamingRPCShrtCannotCall(t *testing.T) {
	cat := catalogtest.Shop()
	watch := ""
	for _, m := range cat.Methods() {
		if m.Streaming() {
			watch = m.FullName
		}
	}
	if watch == "" {
		t.Fatal("the shop fixture carries a streaming rpc")
	}
	lib := contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/contract/v1",
		Domain:     "orders",
		RPCs:       map[string]*contract.RPCContract{watch: {}},
	}})
	report := contract.MeasurePhase(lib, cat, "", contract.PhaseAll)
	for _, r := range report.RPCs {
		if strings.EqualFold(r.RPC, watch) {
			t.Fatalf("%s is out of scope for a unary-only tool, so it is not a gap, got %+v", watch, r)
		}
	}
	without := contract.MeasurePhase(contract.NewLibrary(nil), cat, "", contract.PhaseAll)
	if report.TotalScore != without.TotalScore {
		t.Fatalf("an entry for %s moved the score from %d to %d; a streaming rpc is not measured", watch, without.TotalScore, report.TotalScore)
	}
}

func TestPlanOfAServerStreamingRPCSaysInOneGapLineThatOnlyItsFirstMessageIsRead(t *testing.T) {
	cat := catalogtest.Shop()
	watch := ""
	for _, m := range cat.Methods() {
		if m.ServerStreaming && m.StreamRefusal() == "" {
			watch = m.FullName
		}
	}
	p, err := contract.BuildPlanWith([]string{watch}, contract.NewLibrary(nil), cat, "watch", contract.PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := watch + ": server-streaming; only its first message is read, so what it sends later (updates on change) is not checked"
	gaps := p.GapNotes()
	if len(gaps) == 0 || !slices.Contains(gaps, want) {
		t.Fatalf("want the gap %q, got %v", want, gaps)
	}
	unary, err := contract.BuildPlanWith([]string{"shop.orders.v1.OrderService/FetchOrder"}, contract.NewLibrary(nil), cat, "fetch", contract.PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range unary.Notes {
		if strings.Contains(n, "server-streaming") {
			t.Fatalf("a unary rpc carries no streaming gap: %q", n)
		}
	}
}
