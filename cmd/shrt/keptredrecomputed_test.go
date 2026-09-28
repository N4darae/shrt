package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestAKeptRedTotalRecomputedFromAnUnassertedPriceIsFiledUnderThePrice(t *testing.T) {
	shop := newFakeShop()
	chdirToFakeShop(t, shop)
	writeFile(t, filepath.Join(".shrt", "chains", "oversell.yaml"), `name: oversell
kept_red:
    - {step: fetch, path: order.lines.0.qty, got: "4"}
steps:
    - id: create_p1
      call: shop.catalog.v1.ProductService/CreateProduct
      body: {name: a, price_minor: 1000, sku: s-a}
      expect:
          - {path: status.code, equals: SUCCESS}
      export: {p1: product.id_product}
    - id: create_order
      call: shop.orders.v1.OrderService/CreateOrder
      body:
          id_customer: c1
          idempotency_key: ${uuid}
          lines: [{id_product: "${p1}", qty: 4}]
      expect:
          - {path: order.total_minor, equals: 4000}
      export: {oid: order.id_order}
    - id: fetch
      call: shop.orders.v1.OrderService/FetchOrder
      body: {id_order: "${oid}"}
      expect:
          - {path: order.lines.0.qty, equals: 9}
`)
	side := filepath.Join(t.TempDir(), "side.json")
	t.Setenv(gateReportEnv, side)
	run := func() (string, error) {
		var err error
		out := captureStdout(t, func() { err = runRun(context.Background(), []string{"oversell", "-quiet"}) })
		return out, err
	}
	if out, err := run(); err != nil || !strings.Contains(out, "FAILED AS PINNED") {
		t.Fatalf("the reference run fails as pinned: %v\n%s", err, out)
	}
	shop.priceBug = true
	if out, err := run(); err == nil || !strings.Contains(out, "NOT AS PINNED") {
		t.Fatalf("a lower total is a new failure: %v\n%s", err, out)
	}
	raw, _ := os.ReadFile(side)
	var got gateSidecar
	if err := json.Unmarshal(raw, &got); err != nil || !got.PinsHeld || got.KeptRed != runner.KeptRedNotAsPinned {
		t.Fatalf("pins held: %v %s", err, raw)
	}
	if len(got.Items) != 2 || got.Items[0].Step != "create_p1" || got.Items[0].Path != "product.price_minor" || got.Items[0].Want != "1000" || got.Items[0].Got != "999" ||
		got.Items[1].Step != "create_order" || got.Items[1].suspect() != "create_p1" {
		t.Errorf("the price create_p1 answered unlike the reference run leads, and the total is filed under it: %+v", got.Items)
	}
}

func TestAnUnattributedItemElsewhereDoesNotClearAKeptRedItemsSuspect(t *testing.T) {
	const total = "order.total_minor"
	chains := []*gateChain{
		{name: "slice-a", failed: true, keptRed: runner.KeptRedNotAsPinned, pinsHeld: true, items: []gateItem{
			{Step: "create_p1", Call: shopCreate, Path: "product.price_minor", Want: "1250", Got: "1249", Failed: true},
			{Step: "create_order", Call: shopOrder, Path: total, Want: "6649", Got: "6644", Reason: reason{Kind: reasonWrite, Step: "create_p1", RPC: shopCreate}, Failed: true}}},
		{name: "slice-b", failed: true, keptRed: runner.KeptRedNotAsPinned, pinsHeld: true, items: []gateItem{
			{Step: "order_too_big", Call: shopOrder, Path: total, Want: "4250", Got: "4246", Failed: true}}},
	}
	settleGate(chains)
	if it := chains[0].items[1]; it.suspect() != "create_p1" {
		t.Errorf("a total recomputed from a changed price stays under the price: %+v", it)
	}
	if want := "create_order (OrderService/CreateOrder) order.total_minor want=6649 got=6644; suspect write create_p1 (ProductService/CreateProduct)"; chains[0].first != want {
		t.Errorf("got %q, want %q", chains[0].first, want)
	}
}
