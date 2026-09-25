package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func TestAuthProbeGapsNameRoleGatedRPCsNeverCalledAsTheLowerProfileAndRPCsNeverCalledWithoutAToken(t *testing.T) {
	cat, lib := shopDemo(t)
	opts := contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}, Logins: []string{"shop.auth.v1.AuthService/Login"}}
	chains := []*chain.Chain{{Name: "a", Steps: []*chain.Step{
		{ID: "create_product", Call: "shop.catalog.v1.ProductService/CreateProduct"},
		{ID: "add_stock", Call: "shop.catalog.v1.StockService/AddStock"},
		{ID: "add_stock_as_clerk", Call: "shop.catalog.v1.StockService/AddStock", Auth: "clerk"},
		{ID: "add_stock_without_token", Call: "shop.catalog.v1.StockService/AddStock", SkipAuth: true},
		{ID: "get_product", Call: "shop.catalog.v1.ProductService/GetProduct"},
		{ID: "login", Call: "shop.auth.v1.AuthService/Login"},
	}}}
	got := map[string]bool{}
	for _, g := range contract.AuthProbeGaps(chains, lib, cat, opts) {
		got[g.Kind+" "+g.RPC] = true
	}
	for _, want := range []string{
		"role shop.catalog.v1.ProductService/CreateProduct",
		"token shop.catalog.v1.ProductService/CreateProduct",
		"token shop.catalog.v1.ProductService/GetProduct",
	} {
		if !got[want] {
			t.Fatalf("want gap %q, got %v", want, got)
		}
	}
	for _, not := range []string{
		"role shop.catalog.v1.StockService/AddStock",
		"token shop.catalog.v1.StockService/AddStock",
		"role shop.catalog.v1.ProductService/GetProduct",
		"token shop.orders.v1.OrderService/CreateOrder",
		"token shop.auth.v1.AuthService/Login",
	} {
		if got[not] {
			t.Fatalf("no gap %q: it is probed, declares no role, is the login, or no chain calls it; got %v", not, got)
		}
	}
	if len(contract.AuthProbeGaps(chains, lib, cat, contract.PlanOptions{})) != 0 {
		t.Fatal("with no auth configured there is nothing to probe")
	}
}
