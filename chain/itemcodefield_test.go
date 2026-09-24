package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAPinOnARefusedLinesAppCodeDeclaresTheRefusal(t *testing.T) {
	defer chain.SetEnvelope("", "")
	defer chain.SetItemEnvelope("")
	chain.SetEnvelope("status.code", "SUCCESS")
	chain.SetItemEnvelope("results[].status.code")

	response := map[string]any{
		"status": map[string]any{"code": "SUCCESS"},
		"results": []any{
			map[string]any{"status": map[string]any{"code": "SUCCESS"}},
			map[string]any{"status": map[string]any{"code": "REJECTED",
				"details": []any{map[string]any{"app_code": float64(1204), "reason": "ProductNotFound"}}}},
		},
	}
	refusals, err := chain.ItemRefusals(response)
	if err != nil || len(refusals) != 1 {
		t.Fatalf("want one refusal, got %v %v", refusals, err)
	}
	cases := []struct {
		name     string
		expect   chain.Expectation
		declares bool
	}{
		{"app_code equals", chain.Expectation{Path: "results.1.status.details.0.app_code", Equals: 1204}, true},
		{"reason equals", chain.Expectation{Path: "results.1.status.details.0.reason", Equals: "ProductNotFound"}, true},
		{"verdict equals", chain.Expectation{Path: "results.1.status.code", Equals: "REJECTED"}, true},
		{"app_code on another line", chain.Expectation{Path: "results.0.status.details.0.app_code", Equals: 1204}, false},
		{"app_code on line 10", chain.Expectation{Path: "results.10.status.details.0.app_code", Equals: 1204}, false},
		{"app_code exists", chain.Expectation{Path: "results.1.status.details.0.app_code", Exists: boolPtr(true)}, false},
		{"app_code not_empty", chain.Expectation{Path: "results.1.status.details.0.app_code", NotEmpty: true}, false},
		{"a non-code field", chain.Expectation{Path: "results.1.id_product", Equals: "prd-x"}, false},
	}
	for _, c := range cases {
		got := len(chain.UndeclaredRefusals(refusals, []chain.Expectation{c.expect})) == 0
		if got != c.declares {
			t.Errorf("%s: declares = %v, want %v", c.name, got, c.declares)
		}
	}
}

func boolPtr(b bool) *bool { return &b }
