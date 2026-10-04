package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func bigListRecord(creates, items int, created, listed func(i int) int) *runner.Record {
	var steps []recStep
	for i := range creates {
		steps = append(steps, idStep(fmt.Sprintf("create_%d", i), shopCreate,
			fmt.Sprintf(`{"product":{"id_product":"p%d","sku":"sku-%d","price_minor":"%d"},%s}`, i, i, created(i), shopOK)))
	}
	list := make([]string, items)
	for i := range list {
		list[i] = fmt.Sprintf(`{"id_product":"p%d","sku":"sku-%d","price_minor":"%d"}`, i, i, listed(i))
	}
	steps = append(steps, idStep("list_products", shopList, `{"products":[`+strings.Join(list, ",")+`],`+shopOK+`}`))
	return shopRecord(steps...)
}

func BenchmarkVerifyBigList(b *testing.B) {
	price := func(i int) int { return 100 + i }
	off := func(n int) func(int) int {
		return func(i int) int { return 100 + i - min(i%2, n/(i+1)) }
	}
	for _, c := range []struct {
		name      string
		spot, rec *runner.Record
	}{
		{"writes", bigListRecord(20, 1199, price, price), bigListRecord(20, 1200, off(1200), off(1200))},
		{"list", bigListRecord(20, 1199, price, price), bigListRecord(20, 1200, price, off(10))},
	} {
		b.Run(c.name, func(b *testing.B) {
			e := &env{cat: catalogtest.Shop()}
			report := diff.Compare(&store.SafeSpot{Chain: "shop", RunID: "spot", Steps: c.spot.Steps}, c.rec)
			b.ReportAllocs()
			for b.Loop() {
				line, _ := verifyVerdict(e, "shop", c.rec, report, nil, false, errors.New("shop: regression"), "")
				if !strings.Contains(line, "DRIFT") || len(verifyItems(e, c.rec, report)) == 0 {
					b.Fatalf("no drift in %s", line)
				}
			}
		})
	}
}
