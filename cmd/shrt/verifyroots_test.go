package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
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
		line, _ := verifyVerdict(&env{cat: catalogtest.Shop()}, "shop", rec, report, false, errors.New("regression: x"), "")
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
	line, _ := verifyVerdict(&env{cat: catalogtest.Shop()}, "shop", rec, report, false, errors.New("regression: x"), "")
	if !strings.Contains(line, "order.total_minor want=4250 got=1750; lines: 2 x 250, 3 x 1250\n") {
		t.Errorf("got:\n%s", line)
	}
}
