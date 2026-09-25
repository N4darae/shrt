package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func aliasWhichFixture() []*chain.Chain {
	return []*chain.Chain{{
		Name: "auth-roles",
		Steps: []*chain.Step{
			{ID: "clerk_create_denied", Call: "shop.v1.ProductService/CreateProduct", Expect: []chain.Expectation{
				{Path: "status.details.0.app_code", Equals: 1603},
			}},
			{ID: "clerk_stock_denied", Call: "shop.v1.StockService/AddStock", Expect: []chain.Expectation{
				{Path: "status.details.0.reason", Equals: "PermissionDenied"},
			}},
		},
	}}
}

func TestWhichCodeFindsStepsAssertingTheReasonSeenWithTheAppCode(t *testing.T) {
	denied := map[string]any{"status": map[string]any{"code": "REJECTED", "details": []any{
		map[string]any{"app_code": float64(1603), "reason": "PermissionDenied"},
	}}}
	if got := strings.Join(chain.CodeAliases("1603", []any{denied}), ","); got != "PermissionDenied" {
		t.Fatalf("1603 and PermissionDenied sit in the same detail, want PermissionDenied as its alias, got %q", got)
	}
	if got := strings.Join(chain.CodeAliases("permissiondenied", []any{denied}), ","); got != "1603" {
		t.Fatalf("the alias works both ways, got %q", got)
	}
	for _, q := range []chain.WhichQuery{
		{Code: "1603", Aliases: chain.CodeAliases("1603", []any{denied})},
		{Code: "PermissionDenied", Aliases: chain.CodeAliases("PermissionDenied", []any{denied})},
	} {
		hits := chain.Which(aliasWhichFixture(), q, chain.WhichOptions{})
		if len(hits) != 1 || len(hits[0].Matches) != 2 {
			t.Fatalf("-code %s: want both steps, the one asserting app_code and the one asserting reason, got %+v", q.Code, hits)
		}
	}
	hits := chain.Which(aliasWhichFixture(), chain.WhichQuery{Code: "1603"}, chain.WhichOptions{})
	if len(hits) != 1 || len(hits[0].Matches) != 1 {
		t.Fatalf("without an alias only the step asserting 1603 matches, got %+v", hits)
	}
}
