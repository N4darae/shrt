package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestTheVerifyHeadlineNamesEachDistinctSuspectOnce(t *testing.T) {
	spotRec := shopRecord(
		shopStep("create", shopCreate, `{"product":{"id_product":"p1","price_minor":"5"}}`),
		shopStep("create_2", shopCreate, `{"product":{"id_product":"p2","price_minor":"7"}}`),
		shopStep("list", shopList, `{"products":[{"id_product":"p1","price_minor":"5"},{"id_product":"p2","price_minor":"7"}]}`),
	)
	spot := &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: spotRec.Steps}
	for _, c := range []struct {
		list, also string
	}{
		{`{"products":[{"id_product":"p2","price_minor":"7"},{"id_product":"p1","price_minor":"6"}]}`,
			"  also: list (ProductService/ListProducts) products same items in another order\n"},
		{`{"products":[{"id_product":"p1","price_minor":"6"},{"id_product":"p2","price_minor":"7"}]}`, ""},
	} {
		rec := shopRecord(
			shopStep("create", shopCreate, `{"product":{"id_product":"p1","price_minor":"6"}}`),
			shopStep("create_2", shopCreate, `{"product":{"id_product":"p2","price_minor":"7"}}`),
			shopStep("list", shopList, c.list),
		)
		report := diff.Compare(spot, rec)
		line, _ := verifyVerdict(&env{cat: catalogtest.Shop()}, "shop", rec, report, nil, false, errors.New("regression: x"), "")
		head, rest, _ := strings.Cut(line, "\n")
		if !strings.Contains(head, "first: create (ProductService/CreateProduct) product.price_minor want=5 got=6") || rest != c.also {
			t.Errorf("want the first change, then %q; got:\n%s", c.also, line)
		}
	}
}

func TestTheVerifyHeadlineShowsTheLinesAChangedTotalIsComputedFrom(t *testing.T) {
	steps := func(total string) *runner.Record {
		return shopRecord(
			shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","price_minor":"250"}}`),
			shopStep("create_product_2", shopCreate, `{"product":{"id_product":"p2","price_minor":"1250"}}`),
			shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","lines":[{"id_product":"p1","qty":"2"},{"id_product":"p2","qty":"3"}],"total_minor":"`+total+`"}}`,
				"create_product", "create_product_2"),
		)
	}
	rec := steps("1750")
	report := diff.Compare(&store.SafeSpot{Chain: "shop", RunID: "spot", Steps: steps("4250").Steps}, rec)
	line, _ := verifyVerdict(&env{cat: catalogtest.Shop()}, "shop", rec, report, nil, false, errors.New("regression: x"), "")
	if !strings.Contains(line, "order.total_minor want=4250 got=1750; lines: 2 x 250, 3 x 1250\n") {
		t.Errorf("got:\n%s", line)
	}
}

func TestAStatusOnlyChangeIsNamedByTheExpectationThatFailed(t *testing.T) {
	spotRec := shopRecord(shopStep("list", shopList, `{"products":[{"id_product":"p1"}]}`))
	spot := &store.SafeSpot{Chain: "shop", RunID: "spot", Volatile: []string{"products"}, Steps: spotRec.Steps}
	rec := shopRecord(shopStep("list", shopList, `{"products":[{"id_product":"p9"}]}`).failing("products.0.id_product", "p1", "p9"))
	report := diff.CompareMasking(spot, rec, nil)
	line, _ := verifyVerdict(&env{cat: catalogtest.Shop()}, "shop", rec, report, nil, false, errors.New("regression: x"), "")
	if !strings.Contains(line, "first: list (ProductService/ListProducts) products.0.id_product want=p1 got=p9") {
		t.Errorf("verify headline got:\n%s", line)
	}
	items := verifyItems(&env{cat: catalogtest.Shop()}, rec, report)
	if len(items) != 1 || items[0].headline() != "products.0.id_product want=p1 got=p9" {
		t.Errorf("gate item got %+v", items)
	}
}

func TestTheGateLeadsWithTheStepVerifyNamesFirst(t *testing.T) {
	rec := shopRecord(
		shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","lines":[{"qty":"4"}]}}`),
		shopStep("replay_same", shopOrder, `{"order":{"id_order":"o2","lines":[{"qty":"4"}]}}`).failing("order.id_order", "o1", "o2"),
		shopStep("replay_other_body", shopOrder, `{"order":{"id_order":"o3","lines":[{"qty":"7"}]}}`),
	)
	report := &diff.Report{Changes: []diff.Change{
		{Step: "replay_other_body", Path: "order.lines.0.qty", Kind: diff.KindChanged, Want: "4", Got: "7"},
		{Step: "replay_same", Path: "order.id_order", Kind: diff.KindChanged, Want: "o1", Got: "o2"},
	}}
	first, _ := firstChange(report, rec)
	items := verifyItems(&env{cat: catalogtest.Shop()}, rec, report)
	if first == nil || first.Step != "replay_same" || len(items) != 2 || items[0].Step != "replay_same" {
		t.Errorf("verify names replay_same first, so the gate item order must lead with it: first %+v, items %+v", first, items)
	}
}

func TestTheVerifyHeadlineKeepsARefusedWriteOfTheSameRpcAsItsOwnRoot(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	steps := func(status, qty string) *runner.Record {
		return shopRecord(
			shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","qty_on_hand":"10"},`+shopOK+`}`),
			shopStep("create_product_2", shopCreate, `{"product":{"id_product":"p2","qty_on_hand":"10"},`+shopOK+`}`),
			shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","lines":[{"id_product":"p1","qty":"2"}]},`+shopOK+`}`, "create_product"),
			shopStep("confirm_order", shopConfirm, `{"order":{"id_order":"o1","status":"`+status+`"},`+shopOK+`}`, "create_order"),
			shopStep("create_order_2", shopOrder, `{"order":{"id_order":"o2","lines":[{"id_product":"p2","qty":"20"}]},`+shopOK+`}`, "create_product_2"),
			shopStep("confirm_short", shopConfirm, `{"status":{"code":"REJECTED","details":[{"app_code":1305}]}}`, "create_order_2"),
			shopStep("get_product_2", shopGet, `{"product":{"id_product":"p2","qty_on_hand":"`+qty+`"},`+shopOK+`}`, "create_product_2"),
		)
	}
	rec := steps("PENDING", "-10")
	report := diff.Compare(&store.SafeSpot{Chain: "shop", RunID: "spot", Steps: steps("CONFIRMED", "10").Steps}, rec)
	line, _ := verifyVerdict(effectsEnv(t), "shop", rec, report, nil, false, errors.New("regression: x"), "")
	if !strings.Contains(line, "  also: get_product_2 (ProductService/GetProduct) product.qty_on_hand want=10 got=-10, after write confirm_short (OrderService/ConfirmOrder)") {
		t.Errorf("the refused confirm moved stock, a root apart from the confirm that answered PENDING; got:\n%s", line)
	}
}

func TestAFailingStepWithAChangeOtherThanOrderLeadsOverAnOrderOnlyFailure(t *testing.T) {
	spotRec := shopRecord(
		shopStep("list", shopList, `{"products":[{"sku":"a"},{"sku":"b"},{"sku":"c"}]}`),
		shopStep("list_all", shopList, `{"products":[{"sku":"a"},{"sku":"b"}]}`),
	)
	rec := shopRecord(
		shopStep("list", shopList, `{"products":[{"sku":"c"},{"sku":"a"},{"sku":"b"}]}`).failing("products.0.sku", "a", "c"),
		shopStep("list_all", shopList, `{"products":[]}`).failing("products", "a", "0"),
	)
	report := diff.Compare(&store.SafeSpot{Chain: "shop", RunID: "spot", Steps: spotRec.Steps}, rec)
	first, _ := firstChange(report, rec)
	items := verifyItems(&env{cat: catalogtest.Shop()}, rec, report)
	if first == nil || first.Step != "list_all" || len(items) == 0 || items[0].Step != "list_all" {
		t.Errorf("the failure that is not only another order leads: first %+v, items %+v", first, items)
	}
}

func TestTheGateQuotesAStringWhoseEdgeSpacesChanged(t *testing.T) {
	if want, got := gatePair("  Customer a  ", "  Customer a"); want != `"  Customer a  "` || got != `"  Customer a"` {
		t.Errorf("got want=%s got=%s", want, got)
	}
}
