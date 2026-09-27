package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func principalRecord(between recStep) *runner.Record {
	product := func(price string) string {
		return `{"product":{"id_product":"p1","price_minor":"` + price + `","qty_on_hand":"6"},` + shopOK + `}`
	}
	refused := shopStep("clerk_add_stock", "shop.catalog.v1.StockService/AddStock", `{"status":{"code":"REJECTED","details":[{"app_code":1603}]}}`, "create_product")
	refused.AuthProfile = "clerk"
	clerk := shopStep("clerk_get", shopGet, product("0"), "create_product").failing("product.price_minor", "500", "0")
	clerk.AuthProfile = "clerk"
	return shopRecord(
		shopStep("create_product", shopCreate, product("500")),
		refused,
		shopStep("add_stock", "shop.catalog.v1.StockService/AddStock", `{"qty_on_hand":"6",`+shopOK+`}`, "create_product"),
		clerk,
		between,
		shopStep("admin_get", shopGet, product("500"), "create_product"),
	)
}

func TestAReadThatDiffersOnlyUnderAnotherProfileIsFiledUnderTheReadAsThatProfile(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	moved := []diff.Change{{Step: "clerk_get", Path: "product.price_minor", Kind: diff.KindChanged, Want: "500", Got: "0"}}
	rec := principalRecord(shopStep("fetch", shopFetch, `{"order":{"id_order":"o1"},`+shopOK+`}`))
	it := changesAttribution(effectsEnv(t), rec, moved).item(gateItem{Step: "clerk_get", Call: shopGet, Path: "product.price_minor"})
	if it.Own != "GetProduct answers product.price_minor differently as clerk than as default" || it.Suspect != "" || it.ownKey() != "ProductService/GetProduct as clerk" {
		t.Errorf("the admin read of the same record agrees with the safe spot, so the read differs by principal: %+v", it)
	}
	rec = principalRecord(shopStep("create_order", shopOrder, `{"order":{"id_order":"o1"},`+shopOK+`}`, "create_product"))
	moved = append(moved, diff.Change{Step: "create_order", Path: "order.id_order", Kind: diff.KindChanged, Want: "o0", Got: "o1"})
	if b := changesAttribution(effectsEnv(t), rec, moved).of("clerk_get", "product.price_minor"); strings.Contains(b.own, "differently as clerk") {
		t.Errorf("a changed write between the two reads leaves the profile unproven: %+v", b)
	}
}

func TestAWriteThatNeitherAnswersNorMovesTheFieldIsNoCandidate(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	rec := principalRecord(shopStep("fetch", shopFetch, `{"order":{"id_order":"o1"},`+shopOK+`}`))
	rec.Steps = rec.Steps[:4]
	moved := []diff.Change{{Step: "clerk_get", Path: "product.price_minor", Kind: diff.KindChanged, Want: "500", Got: "0"}}
	b := changesAttribution(effectsEnv(t), rec, moved).of("clerk_get", "product.price_minor")
	if b.write < 0 || rec.Steps[b.write].ID != "create_product" || strings.Contains(b.why, "add_stock") {
		t.Errorf("AddStock neither answers nor moves price_minor, refused or not, so CreateProduct is the only write: %+v", b)
	}
}

func TestAListGrownByAChangedWriteIsFiledUnderThatWrite(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	const list = "shop.orders.v1.OrderService/ListOrders"
	rec := shopRecord(
		shopStep("create_order", shopOrder, `{"order":{"id_order":"o1"},`+shopOK+`}`),
		shopStep("replay", shopOrder, `{"order":{"id_order":"o2"},`+shopOK+`}`),
		shopStep("list_orders", list, `{"orders":[{"id_order":"o1"},{"id_order":"o2"}],`+shopOK+`}`),
	)
	moved := []diff.Change{
		{Step: "replay", Path: "order.id_order", Kind: diff.KindChanged, Want: "o1", Got: "o2"},
		{Step: "list_orders", Path: "orders", Kind: diff.KindLength, Want: 1, Got: 2},
	}
	b := changesAttribution(effectsEnv(t), rec, moved).of("list_orders", "orders")
	if b.write != 1 || b.own != "" {
		t.Errorf("the added order is the changed replay's, so the list echoes that write: %+v", b)
	}
}
