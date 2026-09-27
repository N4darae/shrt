package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

const stockEffects = `apiVersion: shrt/contract/v1
domain: shop
rpcs:
    shop.catalog.v1.StockService/AddStock:
        fields:
            id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
        effects:
            qty_on_hand:
                increase: qty
    shop.catalog.v1.StockService/AddStockBatch:
        fields:
            lines.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
        effects:
            qty_on_hand:
                increase: lines.qty
    shop.orders.v1.OrderService/CreateOrder:
        fields:
            lines.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
        effects:
            qty_on_hand: none
    shop.orders.v1.OrderService/ConfirmOrder:
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.id_order
        effects:
            qty_on_hand:
                decrease: lines.qty
                of: id_order
    shop.orders.v1.OrderService/CancelOrder:
        effects:
            qty_on_hand:
                restore: CONFIRMED
`

func effectsEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shop.yaml"), []byte(stockEffects), 0o644); err != nil {
		t.Fatal(err)
	}
	cat := catalogtest.Shop()
	lib, broken, err := contract.LoadLibraryIn(dir, cat)
	if err != nil || len(broken) > 0 {
		t.Fatalf("load contracts: %v %v", err, broken)
	}
	return &env{cat: cat, lib: lib, libOK: true}
}

func TestTheOneWriteWhoseContractMovesTheFieldSinceItWasLastReadIsTheSuspect(t *testing.T) {
	const add = "shop.catalog.v1.StockService/AddStock"
	order := func(id, status string) string {
		return `{"order":{"id_order":"` + id + `","status":"` + status + `","lines":[{"id_product":"p1","qty":"2"}]}}`
	}
	cancel := shopStep("cancel_2", shopCancel, order("o2", "CANCELLED"), "create_order_2")
	cancel.Request = json.RawMessage(`{"id_order":"o2"}`)
	build := func(readBetween bool, last recStep) *runner.Record {
		steps := []recStep{
			shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","qty_on_hand":"0"}}`),
			shopStep("add_stock", add, `{"qty_on_hand":"10"}`, "create_product"),
			shopStep("create_order", shopOrder, order("o1", "PENDING"), "create_product"),
			shopStep("create_order_2", shopOrder, order("o2", "PENDING"), "create_product"),
		}
		if readBetween {
			steps = append(steps, shopStep("get_pending", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"10"}}`, "create_product"))
		}
		steps = append(steps, cancel, shopStep("confirm_order", shopConfirm, order("o1", "CONFIRMED"), "create_order"), last)
		return shopRecord(steps...)
	}
	get := shopStep("get", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"7"}}`, "create_product")
	list := shopStep("list", shopList, `{"products":[{"id_product":"p0","qty_on_hand":"1"},{"id_product":"p1","qty_on_hand":"7"}]}`)
	for _, c := range []struct {
		name, step, path string
		between          bool
		last             recStep
		named            bool
	}{
		{"a read after the other movers clears them", "get", "product.qty_on_hand", true, get, true},
		{"an item of a list is linked to the writes naming its id", "list", "products.1.qty_on_hand", true, list, true},
		{"with no read in between the add stays a candidate", "get", "product.qty_on_hand", false, get, false},
		{"with no read in between a list item still has candidates", "list", "products.1.qty_on_hand", false, list, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := build(c.between, c.last)
			moved := []diff.Change{{Step: c.step, Path: c.path, Kind: diff.KindChanged, Want: "8", Got: "7"}}
			b := changesAttribution(effectsEnv(t), rec, moved).of(c.step, c.path)
			switch {
			case c.named && (b.write < 0 || rec.Steps[b.write].ID != "confirm_order" || b.why != "its contract moves qty_on_hand; the other writes on that record answered as before"):
				t.Errorf("confirm_order is the only write since the last read whose contract moves qty_on_hand and that applied: %+v", b)
			case !c.named && (b.write >= 0 || !strings.HasPrefix(b.why, "the write or the read: confirm_order (ConfirmOrder), or earlier ")):
				t.Errorf("confirm_order and add_stock both move qty_on_hand, so the list stays: %+v", b)
			}
		})
	}
}
