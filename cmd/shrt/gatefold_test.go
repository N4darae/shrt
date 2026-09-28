package main

import (
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestAHedgedReadNamesTheWriteThatHeadsTheHedge(t *testing.T) {
	const add = "shop.catalog.v1.StockService/AddStock"
	order := `{"order":{"id_order":"o1","status":"CONFIRMED","lines":[{"id_product":"p1","qty":"2"}]}}`
	rec := shopRecord(
		shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","qty_on_hand":"0"}}`),
		shopStep("add_stock", add, `{"qty_on_hand":"10"}`, "create_product"),
		shopStep("create_order", shopOrder, order, "create_product"),
		shopStep("confirm_order", shopConfirm, order, "create_order"),
		shopStep("get", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"9"}}`, "create_product"),
	)
	moved := []diff.Change{{Step: "get", Path: "product.qty_on_hand", Kind: diff.KindChanged, Want: "8", Got: "9"}}
	it := changesAttribution(effectsEnv(t), rec, moved).item(gateItem{Step: "get", Call: shopGet, Path: "product.qty_on_hand"})
	if it.Reason.Kind != reasonUnclear || it.suspect() != "confirm_order" || it.rpc() != "OrderService/ConfirmOrder" {
		t.Errorf("the hedge is headed by confirm_order, so the gate files the read under ConfirmOrder: %+v", it)
	}
}

func TestTheLoginRpcCarriesNoProfileSuffix(t *testing.T) {
	const login, create = "shrt.test.v1.AuthService/Login", "shrt.test.v1.ThingService/Create"
	e := &env{cat: catalogtest.New(), cfg: &config.Config{Auth: &config.Auth{Call: login}}}
	for _, c := range []struct {
		call, profile, want string
	}{
		{login, runner.NoAuthProfile, ""},
		{create, runner.NoAuthProfile, "as none"},
		{create, "clerk", "as clerk"},
		{create, "", ""},
	} {
		if got := asOf(e, &runner.StepRecord{Call: c.call, AuthProfile: c.profile}); got != c.want {
			t.Errorf("%s %q: got %q, want %q", c.call, c.profile, got, c.want)
		}
	}
}
