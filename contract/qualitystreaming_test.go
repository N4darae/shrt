package contract_test

import (
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
	report := contract.Measure(lib, cat, "")
	for _, r := range report.RPCs {
		if strings.EqualFold(r.RPC, watch) {
			t.Fatalf("%s is out of scope for a unary-only tool, so it is not a gap, got %+v", watch, r)
		}
	}
	if report.TotalScore != 0 {
		t.Fatalf("want 0, got %d", report.TotalScore)
	}
}
