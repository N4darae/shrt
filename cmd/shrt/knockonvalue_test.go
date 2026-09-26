package main

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

const shopOK = `"status":{"code":"SUCCESS"}`

func TestATotalThatRecomputesFromChangedPricesIsFiledUnderThePriceWrite(t *testing.T) {
	order := func(total string) string {
		return `{"order":{"id_order":"o1","lines":[{"id_product":"p1","qty":"2"},{"id_product":"p2","qty":"3"}],"total_minor":"` + total + `"},` + shopOK + `}`
	}
	rec := func(total string) *runner.Record {
		return shopRecord(
			shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","price_minor":"249"},`+shopOK+`}`).failing("product.price_minor", "250", "249"),
			shopStep("create_product_2", shopCreate, `{"product":{"id_product":"p2","price_minor":"1000"},`+shopOK+`}`),
			shopStep("create_order", shopOrder, order(total), "create_product", "create_product_2").failing("order.total_minor", "3500", total),
			shopStep("fetch_order", shopFetch, order(total), "create_order").failing("order.total_minor", "3500", total),
		)
	}
	for _, step := range []string{"create_order", "fetch_order"} {
		if write, own, _ := blameOf(t, rec("3498"), step, "order.total_minor"); write != "create_product" || own != "" {
			t.Errorf("%s: 2 x 249 + 3 x 1000 is the total the changed price gives, got write %q own %q", step, write, own)
		}
	}
	if write, _, _ := blameOf(t, rec("3497"), "create_order", "order.total_minor"); write != "" {
		t.Errorf("a total the prices do not give is the order write's own, got write %q", write)
	}
	if write, _, _ := blameOf(t, rec("3497"), "fetch_order", "order.total_minor"); write != "create_order" {
		t.Errorf("a read answering what the order write answered stays on the order write, got write %q", write)
	}
}

func TestAListGainingItemsIsNotAKnockOnOfAChangedValue(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	list := shopStep("list", shopList, `{"products":[{"id_product":"p9"},{"id_product":"p1"}],`+shopOK+`}`).failing("products.1", false, true)
	list.Expect[0].Rule = "exists"
	rec := shopRecord(
		shopStep("create", shopCreate, `{"product":{"id_product":"p1","price_minor":"249"},`+shopOK+`}`).failing("product.price_minor", "250", "249"),
		list,
	)
	write, own, _ := blameOf(t, rec, "list", "products.1")
	if write != "" || own != "ListProducts answers another set of products" {
		t.Errorf("an added item is the list's own, whatever value changed before it: got write %q own %q", write, own)
	}
}

func TestARefusalAfterValueChangesIsTheReadsOwnAndNamesTheProfile(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	order := `{"order":{"id_order":"o1","total_minor":"7"},` + shopOK + `}`
	refused := shopStep("fetch_as_clerk", shopFetch, `{"status":{"code":"REJECTED","details":[{"app_code":1302,"reason":"OrderNotFound"}]}}`, "create_order").
		failing("status.code", "SUCCESS", "REJECTED")
	refused.AuthProfile = "clerk"
	rec := shopRecord(
		shopStep("create_order", shopOrder, order).failing("order.total_minor", "9", "7"),
		shopStep("fetch", shopFetch, order, "create_order").failing("order.total_minor", "9", "7"),
		refused,
	)
	write, own, _ := blameOf(t, rec, "fetch_as_clerk", "status.code")
	if write != "" || own != "FetchOrder passes as default, refused as clerk (1302 OrderNotFound)" {
		t.Errorf("got write %q own %q", write, own)
	}
	rec.Steps[0].Expect[0] = chain.ExpectResult{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "REJECTED"}
	rec.Steps[0].Response = []byte(`{"status":{"code":"REJECTED"}}`)
	if write, own, _ := blameOf(t, rec, "fetch_as_clerk", "status.code"); own != "" || write != "create_order" {
		t.Errorf("a refusal after a refused write stays on that write: got write %q own %q", write, own)
	}
}

func TestAKnockOnNeedsTheValueTheEarlierWriteAnswered(t *testing.T) {
	rec := shopRecord(
		shopStep("create", shopCreate, `{"product":{"id_product":"p1","price_minor":"249"}}`).failing("product.price_minor", "250", "249"),
		shopStep("list", shopList, `{"products":[{"id_product":"p7","price_minor":"249"}]}`).failing("products.0.price_minor", "250", "249"),
		shopStep("list_2", shopList, `{"products":[{"id_product":"p7","price_minor":"5"}]}`).failing("products.0.price_minor", "4", "5"),
	)
	b := runAttribution(nil, rec).of("list", "products.0.price_minor")
	if b.write != 0 || !b.knock {
		t.Errorf("the value the write answered is a knock-on of it: %+v", b)
	}
	if b := runAttribution(nil, rec).of("list_2", "products.0.price_minor"); b.write >= 0 {
		t.Errorf("another value is not explained by the write: %+v", b)
	}
}

func TestAStreamedMessageCarryingTheValueAWriteAnsweredIsFiledUnderTheWrite(t *testing.T) {
	order := `{"order":{"id_order":"o1","total_minor":"1750"}}`
	rec := shopRecord(
		shopStep("create_order", shopOrder, order).failing("order.total_minor", "4250", "1750"),
		shopStep("watch_order", "shop.orders.v1.OrderService/WatchOrder", `{"messages":[`+order+`]}`, "create_order").
			failing("messages.0.order.total_minor", "4250", "1750"),
	)
	if write, own, _ := blameOf(t, rec, "watch_order", "messages.0.order.total_minor"); write != "create_order" || own != "" {
		t.Errorf("a streamed read answering the total the write answered is a knock-on of the write, got write %q own %q", write, own)
	}
}

func TestAChangedTotalShowsTheLinesItIsComputedFromOnce(t *testing.T) {
	order := `{"order":{"id_order":"o1","lines":[{"id_product":"p1","qty":"2"},{"id_product":"p2","qty":"3"}],"total_minor":"1750"}}`
	rec := shopRecord(
		shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","price_minor":"250"}}`),
		shopStep("create_product_2", shopCreate, `{"product":{"id_product":"p2","price_minor":"1250"}}`),
		shopStep("create_order", shopOrder, order, "create_product", "create_product_2").failing("order.total_minor", "4250", "1750"),
	)
	it := runAttribution(nil, rec).item(gateItem{Step: "create_order", Call: shopOrder, Path: "order.total_minor", Rule: "equals", Want: "4250", Got: "1750"})
	if want := "order.total_minor want=4250 got=1750; lines: 2 x 250, 3 x 1250"; it.headline() != want {
		t.Errorf("got %q, want %q", it.headline(), want)
	}
	rec.Steps[2].Expect[0].Want = "4000"
	if it := runAttribution(nil, rec).item(gateItem{Step: "create_order", Call: shopOrder, Path: "order.total_minor"}); it.Inputs != "" {
		t.Errorf("a want the lines do not give names no inputs, got %q", it.Inputs)
	}
}
