package main

import (
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

const shopBatch = "shop.catalog.v1.StockService/AddStockBatch"

func TestAStepHeldBackByAReadIsNeverFiledUnderThatReadAsAWrite(t *testing.T) {
	product := `{"product":{"id_product":"p1","qty_on_hand":"7"}}`
	rec := shopRecord(
		shopStep("get", shopGet, product).failing("product.qty_on_hand", "8", "7"),
		shopStep("get_again", shopGet, product).heldBy("get", "product.qty_on_hand"),
	)
	b := runAttribution(&env{cat: catalogtest.Shop()}, rec).of("get_again", "product.qty_on_hand")
	if b.Kind != reasonKnockOn || b.Step != "" || b.Read != "get" || b.String() != "knock-on of read get (ProductService/GetProduct)" {
		t.Errorf("the read it waits on is no write: %+v", b)
	}

	order := `{"order":{"id_order":"o1","status":"CONFIRMED","lines":[{"id_product":"p1","qty":"2"}]}}`
	rec = shopRecord(
		shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","qty_on_hand":"0"}}`),
		shopStep("add_stock", "shop.catalog.v1.StockService/AddStock", `{"qty_on_hand":"10"}`, "create_product"),
		shopStep("create_order", shopOrder, order, "create_product"),
		shopStep("confirm_order", shopConfirm, order, "create_order"),
		shopStep("get", shopGet, product, "create_product").failing("product.qty_on_hand", "8", "7"),
		shopStep("get_again", shopGet, product, "create_product").heldBy("get", "product.qty_on_hand"),
	)
	moved := []diff.Change{{Step: "get", Path: "product.qty_on_hand", Kind: diff.KindChanged, Want: "8", Got: "7"}}
	b = changesAttribution(effectsEnv(t), rec, moved).of("get_again", "product.qty_on_hand")
	if b.Kind != reasonKnockOn || b.Step != "confirm_order" {
		t.Errorf("the step waits on the write the read observes: %+v", b)
	}
}

func TestAnAuthProbeFailingItsOwnTransportExpectationIsItsOwnSuspect(t *testing.T) {
	order := `{"order":{"id_order":"o1"}}`
	for _, profile := range []string{runner.NoAuthProfile, chain.InvalidTokenAuth} {
		probe := shopStep("watch_without_token", "shop.orders.v1.OrderService/WatchOrder", order, "create_order").failing("transport.code", "unauthenticated", "ok")
		probe.AuthProfile = profile
		rec := shopRecord(shopStep("create_order", shopOrder, order), probe)
		write, own, _ := blameOf(t, rec, "watch_without_token", "transport.code")
		if write != "" || own != reasonProbe {
			t.Errorf("%s: got write %q own %q", profile, write, own)
		}
	}
}

func TestARefusalFollowingAnEarlierChangedWriteOnItsRecordIsFiledUnderThatWrite(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	refused := `{"status":{"code":"REJECTED","details":[{"app_code":1305}]},"order":{"id_order":"o1","status":"ORDER_STATUS_PENDING"}}`
	order := `{"order":{"id_order":"o1","status":"ORDER_STATUS_PENDING","lines":[{"id_product":"p1","qty":"3"}]},` + shopOK + `}`
	build := func(batch recStep, between ...recStep) *runner.Record {
		steps := []recStep{
			shopStep("create_a", shopCreate, `{"product":{"id_product":"p1"},`+shopOK+`}`),
			shopStep("create_b", shopCreate, `{"product":{"id_product":"p2"},`+shopOK+`}`),
			batch,
		}
		steps = append(steps, between...)
		steps = append(steps,
			shopStep("create_order", shopOrder, order, "create_a", "create_b"),
			shopStep("confirm", shopConfirm, refused, "create_order").
				failing("status.code", "SUCCESS", "REJECTED").failing("order.status", "ORDER_STATUS_CONFIRMED", "ORDER_STATUS_PENDING"),
		)
		return shopRecord(steps...)
	}
	suspect := func(rec *runner.Record, path string) string {
		return pinnedAttribution(effectsEnv(t), rec, nil).of("confirm", path).blamed("confirm")
	}

	moved := build(shopStep("batch", shopBatch, `{"status":{"code":"REJECTED","details":[{"app_code":1203}]}}`, "create_a", "create_b").
		failing("status.code", "SUCCESS", "REJECTED"))
	for _, path := range []string{"status.code", "order.status"} {
		if got := suspect(moved, path); got != "batch" {
			t.Errorf("verdict moved, %s: got %q, want batch", path, got)
		}
	}

	answered := shopStep("batch", shopBatch, `{"results":[{"id_product":"p1","qty_on_hand":"7"}],`+shopOK+`}`, "create_a")
	read := shopStep("get_a", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"4"},`+shopOK+`}`, "create_a").failing("product.qty_on_hand", "7", "4")
	if got := suspect(build(answered, read), "status.code"); got != "batch" {
		t.Errorf("a read of the stock the confirm needs changed after the batch: got %q, want batch", got)
	}
	email := shopStep("get_a", shopGet, `{"product":{"id_product":"p1","name":"x"},`+shopOK+`}`, "create_a").failing("product.name", "X", "x")
	if got := suspect(build(answered, email), "status.code"); got != "" {
		t.Errorf("a changed field the confirm's contract does not move explains nothing: got %q", got)
	}
}
