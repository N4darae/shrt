package main

import (
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/store"
)

func TestAnOrderChangeNamesTheKeyTheNewOrderFollows(t *testing.T) {
	spot := shopRecord(shopStep("list", shopList, `{"products":[{"id_product":"p1","sku":"a","name":"c"},{"id_product":"p2","sku":"b","name":"a"},{"id_product":"p3","sku":"c","name":"b"}]}`))
	for _, c := range []struct{ now, want string }{
		{`{"products":[{"id_product":"p2","sku":"b","name":"a"},{"id_product":"p3","sku":"c","name":"b"},{"id_product":"p1","sku":"a","name":"c"}]}`, "same items in another order (now by name, was by sku)"},
		{`{"products":[{"id_product":"p3","sku":"c","name":"b"},{"id_product":"p1","sku":"a","name":"c"},{"id_product":"p2","sku":"b","name":"a"}]}`, "same items in another order"},
	} {
		rec := shopRecord(shopStep("list", shopList, c.now))
		report := diff.Compare(&store.SafeSpot{Chain: "shop", RunID: "spot", Steps: spot.Steps}, rec)
		items := verifyItems(&env{cat: catalogtest.Shop()}, rec, report)
		if len(items) == 0 {
			t.Fatalf("no items for %s", c.now)
		}
		if _, got := items[0].shown(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}
