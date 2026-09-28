package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

type recStep struct {
	*runner.StepRecord
}

func shopStep(id, call, response string, refs ...string) recStep {
	st := &runner.StepRecord{ID: id, Call: call, Status: runner.StatusPassed, Response: json.RawMessage(response), BodyRefs: map[string]string{}}
	for i, r := range refs {
		st.BodyRefs[string(rune('a'+i))] = "${" + r + ".x}"
	}
	return recStep{st}
}

func (s recStep) failing(path string, want, got any) recStep {
	s.Status = runner.StatusFailed
	s.Expect = append(s.Expect, chain.ExpectResult{Path: path, Rule: "equals", Want: want, Got: got})
	return s
}

func (s recStep) heldBy(src, path string) recStep {
	s.Status = runner.StatusFailed
	s.Expect = append(s.Expect, chain.ExpectResult{Path: path, Rule: "unevaluated",
		Detail: `not evaluated: ${` + src + `.` + path + `} reads step "` + src + `", which did not pass`})
	return s
}

func (s recStep) as(profile string) recStep {
	s.AuthProfile = profile
	return s
}

func (s recStep) with(f func(*runner.StepRecord)) recStep {
	f(s.StepRecord)
	return s
}

func shopRecord(steps ...recStep) *runner.Record {
	rec := &runner.Record{}
	for _, s := range steps {
		rec.Steps = append(rec.Steps, s.StepRecord)
	}
	return rec
}

const (
	shopCreate  = "shop.catalog.v1.ProductService/CreateProduct"
	shopGet     = "shop.catalog.v1.ProductService/GetProduct"
	shopList    = "shop.catalog.v1.ProductService/ListProducts"
	shopOrder   = "shop.orders.v1.OrderService/CreateOrder"
	shopConfirm = "shop.orders.v1.OrderService/ConfirmOrder"
	shopCancel  = "shop.orders.v1.OrderService/CancelOrder"
	shopFetch   = "shop.orders.v1.OrderService/FetchOrder"
	shopAdd     = "shop.catalog.v1.StockService/AddStock"
	shopBatch   = "shop.catalog.v1.StockService/AddStockBatch"
	shopWatch   = "shop.orders.v1.OrderService/WatchOrder"
	shopOK      = `"status":{"code":"SUCCESS"}`
)

func blameOf(t *testing.T, rec *runner.Record, step, path string) (string, string, string) {
	t.Helper()
	r := runAttribution(&env{cat: catalogtest.Shop()}, rec).of(step, path)
	own, cascade := "", ""
	if r.Kind == reasonKnockOn {
		cascade = r.Kind
	} else if !r.blames() {
		own = r.Kind
	}
	return r.blamed(step), own, cascade
}

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
        needs: [shop.catalog.v1.StockService/AddStock]
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

func idStep(id, call, response string, refs ...string) recStep {
	st := shopStep(id, call, response)
	for i, r := range refs {
		st.BodyRefs[string(rune('a'+i))] = "${" + r + ".product.id_product}"
	}
	return st
}

func (s recStep) held(path string, v any) recStep {
	s.Expect = append(s.Expect, chain.ExpectResult{Path: path, Rule: "equals", Want: v, Got: v, Passed: true})
	return s
}

func principalRecord(between recStep) *runner.Record {
	product := func(price string) string {
		return `{"product":{"id_product":"p1","price_minor":"` + price + `","qty_on_hand":"6"},` + shopOK + `}`
	}
	return shopRecord(
		shopStep("create_product", shopCreate, product("500")),
		shopStep("clerk_add_stock", shopAdd, `{"status":{"code":"REJECTED","details":[{"app_code":1603}]}}`, "create_product").as("clerk"),
		shopStep("add_stock", shopAdd, `{"qty_on_hand":"6",`+shopOK+`}`, "create_product"),
		shopStep("clerk_get", shopGet, product("0"), "create_product").failing("product.price_minor", "500", "0").as("clerk"),
		between,
		shopStep("admin_get", shopGet, product("500"), "create_product"),
	)
}

type attrCase struct {
	name     string
	envelope bool
	env      string
	moved    []diff.Change
	pinned   bool
	rec      func() *runner.Record
	step     string
	path     string
	kind     string
	blamed   string
	check    func(reason) bool
}

func is(want string, got string) bool {
	if strings.HasPrefix(want, "!") {
		return got != want[1:]
	}
	return want == "?" || got == want
}

func changed(step, path string, want, got any) []diff.Change {
	return []diff.Change{{Step: step, Path: path, Kind: diff.KindChanged, Want: want, Got: got}}
}

func TestAttributionNamesTheSuspectByKind(t *testing.T) {
	order7 := `{"order":{"id_order":"o1","total_minor":"7"}}`
	orderAt := func(total string) string {
		return `{"order":{"id_order":"o1","lines":[{"id_product":"p1","qty":"2"},{"id_product":"p2","qty":"3"}],"total_minor":"` + total + `"},` + shopOK + `}`
	}
	priceTotal := func(total string) func() *runner.Record {
		return func() *runner.Record {
			return shopRecord(
				shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","price_minor":"249"},`+shopOK+`}`).failing("product.price_minor", "250", "249"),
				shopStep("create_product_2", shopCreate, `{"product":{"id_product":"p2","price_minor":"1000"},`+shopOK+`}`),
				shopStep("create_order", shopOrder, orderAt(total), "create_product", "create_product_2").failing("order.total_minor", "3500", total),
				shopStep("fetch_order", shopFetch, orderAt(total), "create_order").failing("order.total_minor", "3500", total),
			)
		}
	}
	fiveOrders := func() *runner.Record {
		return shopRecord(
			shopStep("create_order", shopOrder, order7).failing("order.total_minor", "9", "7"),
			shopStep("confirm_order", shopConfirm, order7, "create_order").failing("order.total_minor", "9", "7"),
			shopStep("fetch_after_confirm", shopFetch, order7, "create_order").failing("order.total_minor", "9", "7"),
			shopStep("cancel_order", shopCancel, order7, "create_order").failing("order.total_minor", "9", "7"),
			shopStep("fetch_order_after_cancel_order", shopFetch, order7, "create_order").failing("order.total_minor", "9", "7"),
		)
	}
	wrongLevel := func() *runner.Record {
		product := `{"product":{"id_product":"p1","price_minor":"6"}}`
		order := `{"order":{"id_order":"o1"}}`
		return shopRecord(
			shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","price_minor":"5"}}`),
			shopStep("create_order", shopOrder, order, "create_product"),
			shopStep("confirm_order", shopConfirm, order, "create_order"),
			shopStep("get_product_after_confirm_order", shopGet, product, "create_product").failing("product.price_minor", "5", "6"),
			shopStep("cancel_order", shopCancel, order, "create_order"),
			shopStep("get_product_after_cancel_order", shopGet, product, "create_product").failing("product.price_minor", "5", "6"),
		)
	}
	clerkHeld := func(clerk string) func() *runner.Record {
		product := func(price string) string {
			return `{"product":{"id_product":"p1","price_minor":"` + price + `"},` + shopOK + `}`
		}
		return func() *runner.Record {
			return shopRecord(shopStep("create", shopCreate, product("1249")).failing("product.price_minor", "1250", "1249"),
				shopStep("get", shopGet, product("1249"), "create").failing("product.price_minor", "1250", "1249"),
				shopStep("get_as_clerk", shopGet, product(clerk), "create").as("clerk").heldBy("get", "product.price_minor").with(func(st *runner.StepRecord) {
					st.Expect[0].Want, st.Expect[0].Got = "1249", clerk
				}))
		}
	}
	otherItems := func(writeFailed bool) func() *runner.Record {
		return func() *runner.Record {
			rec := shopRecord(
				shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`),
				shopStep("create_2", shopCreate, `{"product":{"id_product":"p2"}}`),
				shopStep("list", shopList, `{"products":[{"id_product":"p9"},{"id_product":"p1"},{"id_product":"p2"}]}`, "create", "create_2").
					failing("products.0.id_product", "p1", "p9"),
			)
			rec.Steps[2].Expect = append(rec.Steps[2].Expect, chain.ExpectResult{Path: "products.2", Rule: "exists", Want: false, Got: true})
			if writeFailed {
				rec.Steps[1].Status = runner.StatusFailed
			}
			return rec
		}
	}
	transport := func(tail ...int) func() *runner.Record {
		return func() *runner.Record {
			add := shopStep("add", shopAdd, `{"code":"unavailable"}`, "create")
			add.Status, add.HTTPStatus, add.Transport = runner.StatusError, 503, &runner.TransportError{Code: "unavailable", Message: "busy"}
			order := `{"order":{"id_order":"o1","lines":[{"id_product":"p1","qty":"3"}]}}`
			all := []recStep{
				shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`),
				shopStep("create_2", shopCreate, `{"product":{"id_product":"p2"}}`),
				add,
				shopStep("create_order", shopOrder, order, "create", "create_2").failing("order.lines.0.qty", "2", "3"),
				shopStep("confirm_order", shopConfirm, order, "create_order").failing("status.code", "SUCCESS", "REJECTED"),
				shopStep("get", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"0"}}`, "create").failing("product.qty_on_hand", "5", "0"),
				shopStep("get_2", shopGet, `{"product":{"id_product":"p2","qty_on_hand":"0"}}`, "create_2").failing("product.qty_on_hand", "5", "0"),
			}
			if len(tail) == 0 {
				return shopRecord(all...)
			}
			var picked []recStep
			for _, i := range tail {
				picked = append(picked, all[i])
			}
			return shopRecord(picked...)
		}
	}
	sameRPC := func() *runner.Record {
		return shopRecord(
			shopStep("create_a", shopCreate, `{"product":{"id_product":"p1"}}`),
			shopStep("create_b", shopCreate, `{"product":{"id_product":"p2"}}`),
			shopStep("add_zero", shopAdd, `{"qty_on_hand":"1"}`, "create_a").failing("qty_on_hand", "0", "1"),
			shopStep("add_as_other", shopAdd, `{"qty_on_hand":"10"}`, "create_b").failing("qty_on_hand", "0", "10"),
			shopStep("add_more_to_a", shopAdd, `{"qty_on_hand":"11"}`, "create_a").failing("qty_on_hand", "10", "11"),
		)
	}
	laterRead := func(response string, swap bool) func() *runner.Record {
		return func() *runner.Record {
			rec := shopRecord(
				shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","total_minor":"9"}}`),
				shopStep("confirm_order", shopConfirm, `{"order":{"id_order":"o1","total_minor":"7"}}`, "create_order").failing("order.total_minor", "9", "7"),
				shopStep("fetch_order", shopFetch, response, "create_order").held("order.total_minor", "9"),
				shopStep("cancel_order", shopCancel, `{"order":{"id_order":"o1","total_minor":"0"}}`, "create_order"),
			)
			if swap {
				rec.Steps[2], rec.Steps[3] = rec.Steps[3], rec.Steps[2]
			}
			return rec
		}
	}
	earlierField := func() *runner.Record {
		order := `{"order":{"id_order":"o1","status":"ORDER_STATUS_PENDING"},` + shopOK + `}`
		return shopRecord(
			shopStep("create_product", shopCreate, `{"product":{"id_product":"p1"},`+shopOK+`}`),
			shopStep("add_stock", shopAdd, `{"qty_on_hand":"9",`+shopOK+`}`, "create_product").failing("qty_on_hand", "10", "9"),
			shopStep("create_order", shopOrder, order, "create_product"),
			shopStep("confirm_order", shopConfirm, order, "create_order"),
			shopStep("get_product_after_confirm_order", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"7"},`+shopOK+`}`, "create_product").failing("product.qty_on_hand", "8", "7"),
			shopStep("confirm_exact", shopConfirm, `{"status":{"code":"REJECTED","details":[{"app_code":1305}]}}`, "create_order").failing("status.code", "SUCCESS", "REJECTED"),
			shopStep("fetch_order_after_confirm_exact", shopFetch, order, "create_order").failing("order.status", "ORDER_STATUS_CONFIRMED", "ORDER_STATUS_PENDING"),
		)
	}
	ownFlip := func(write, call, request string) func() *runner.Record {
		return func() *runner.Record {
			order := `{"order":{"id_order":"o1","total_minor":"747"},` + shopOK + `}`
			return shopRecord(
				shopStep("create_product", write, `{"product":{"id_product":"p1","price_minor":"249"},`+shopOK+`}`).failing("product.price_minor", "250", "249"),
				shopStep("create_order", shopOrder, order, "create_product").failing("order.total_minor", "750", "747"),
				shopStep("flip", call, `{"status":{"code":"REJECTED","details":[{"app_code":1304,"reason":"OrderCancelled"}]}}`, "create_order").
					failing("status.code", "SUCCESS", "REJECTED").with(func(st *runner.StepRecord) { st.Request = json.RawMessage(request) }),
			)
		}
	}
	listOrder := func(readMoved bool) func() *runner.Record {
		return func() *runner.Record {
			fetched := shopStep("fetch_order", shopFetch, `{"order":{"id_order":"o1","lines":[{"id_product":"a","qty":"2"},{"id_product":"b","qty":"3"}]}}`, "create_order").held("order.lines.0.qty", "2")
			fetched.Expect[0].Passed = !readMoved
			return shopRecord(
				shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","lines":[{"id_product":"b","qty":"3"},{"id_product":"a","qty":"2"}]}}`).failing("order.lines.0.qty", "2", "3"),
				fetched,
			)
		}
	}
	envelopeBatch := func() *runner.Record {
		return shopRecord(
			shopStep("create_product", shopCreate, `{"product":{"id_product":"p1"},`+shopOK+`}`),
			shopStep("batch", shopBatch, `{"status":{"code":"REJECTED"},"results":[{"id_product":"p1","status":{"code":"REJECTED"}}]}`, "create_product").failing("status.code", "SUCCESS", "REJECTED"),
			shopStep("get_product", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"0"},`+shopOK+`}`, "create_product").held("status.code", "SUCCESS"),
		)
	}
	laterWrite := func() *runner.Record {
		return shopRecord(
			shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`).failing("product.created_at", "t", nil),
			shopStep("add_negative", shopAdd, `{"qty_on_hand":"0",`+shopOK+`}`, "create").failing("status.code", "REJECTED", "SUCCESS"),
			shopStep("add_large", shopAdd, `{"qty_on_hand":"1250",`+shopOK+`}`, "create").failing("qty_on_hand", "1251", "1250"),
			shopStep("add_larger", shopAdd, `{"qty_on_hand":"13595",`+shopOK+`}`, "create").failing("qty_on_hand", "13596", "13595"),
			shopStep("create_other", shopCreate, `{"product":{"id_product":"p2"}}`),
			shopStep("add_other", shopAdd, `{"qty_on_hand":"3",`+shopOK+`}`, "create_other").failing("qty_on_hand", "4", "3"),
		)
	}
	refusedAfter := func(writeRefused bool) func() *runner.Record {
		return func() *runner.Record {
			order := `{"order":{"id_order":"o1","total_minor":"7"},` + shopOK + `}`
			rec := shopRecord(
				shopStep("create_order", shopOrder, order).failing("order.total_minor", "9", "7"),
				shopStep("fetch", shopFetch, order, "create_order").failing("order.total_minor", "9", "7"),
				shopStep("fetch_as_clerk", shopFetch, `{"status":{"code":"REJECTED","details":[{"app_code":1302,"reason":"OrderNotFound"}]}}`, "create_order").
					failing("status.code", "SUCCESS", "REJECTED").as("clerk"),
			)
			if writeRefused {
				rec.Steps[0].Expect[0] = chain.ExpectResult{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "REJECTED"}
				rec.Steps[0].Response = []byte(`{"status":{"code":"REJECTED"}}`)
			}
			return rec
		}
	}
	knockValue := func() *runner.Record {
		return shopRecord(
			shopStep("create", shopCreate, `{"product":{"id_product":"p1","price_minor":"249"}}`).failing("product.price_minor", "250", "249"),
			shopStep("list", shopList, `{"products":[{"id_product":"p7","price_minor":"249"}]}`).failing("products.0.price_minor", "250", "249"),
			shopStep("list_2", shopList, `{"products":[{"id_product":"p7","price_minor":"5"}]}`).failing("products.0.price_minor", "4", "5"),
		)
	}
	heldRead := func() *runner.Record {
		product := `{"product":{"id_product":"p1","qty_on_hand":"7"}}`
		return shopRecord(
			shopStep("get", shopGet, product).failing("product.qty_on_hand", "8", "7"),
			shopStep("get_again", shopGet, product).heldBy("get", "product.qty_on_hand"),
		)
	}
	heldReadAfterConfirm := func() *runner.Record {
		product := `{"product":{"id_product":"p1","qty_on_hand":"7"}}`
		order := `{"order":{"id_order":"o1","status":"CONFIRMED","lines":[{"id_product":"p1","qty":"2"}]}}`
		return shopRecord(
			shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","qty_on_hand":"0"}}`),
			shopStep("add_stock", shopAdd, `{"qty_on_hand":"10"}`, "create_product"),
			shopStep("create_order", shopOrder, order, "create_product"),
			shopStep("confirm_order", shopConfirm, order, "create_order"),
			shopStep("get", shopGet, product, "create_product").failing("product.qty_on_hand", "8", "7"),
			shopStep("get_again", shopGet, product, "create_product").heldBy("get", "product.qty_on_hand"),
		)
	}
	probe := func(profile string) func() *runner.Record {
		return func() *runner.Record {
			order := `{"order":{"id_order":"o1"}}`
			return shopRecord(shopStep("create_order", shopOrder, order),
				shopStep("watch_without_token", shopWatch, order, "create_order").failing("transport.code", "unauthenticated", "ok").as(profile))
		}
	}
	confirmAfter := func(batch recStep, between ...recStep) func() *runner.Record {
		return func() *runner.Record {
			refused := `{"status":{"code":"REJECTED","details":[{"app_code":1305}]},"order":{"id_order":"o1","status":"ORDER_STATUS_PENDING"}}`
			order := `{"order":{"id_order":"o1","status":"ORDER_STATUS_PENDING","lines":[{"id_product":"p1","qty":"3"}]},` + shopOK + `}`
			steps := append([]recStep{
				shopStep("create_a", shopCreate, `{"product":{"id_product":"p1"},`+shopOK+`}`),
				shopStep("create_b", shopCreate, `{"product":{"id_product":"p2"},`+shopOK+`}`),
				batch,
			}, between...)
			return shopRecord(append(steps,
				shopStep("create_order", shopOrder, order, "create_a", "create_b"),
				shopStep("confirm", shopConfirm, refused, "create_order").
					failing("status.code", "SUCCESS", "REJECTED").failing("order.status", "ORDER_STATUS_CONFIRMED", "ORDER_STATUS_PENDING"),
			)...)
		}
	}
	movedBatch := func() recStep {
		return shopStep("batch", shopBatch, `{"status":{"code":"REJECTED","details":[{"app_code":1203}]}}`, "create_a", "create_b").failing("status.code", "SUCCESS", "REJECTED")
	}
	answeredBatch := func() recStep {
		return shopStep("batch", shopBatch, `{"results":[{"id_product":"p1","qty_on_hand":"7"}],`+shopOK+`}`, "create_a")
	}
	profileWrite := func(changedItself bool) func() *runner.Record {
		return func() *runner.Record {
			product := func(qty string) string {
				return `{"product":{"id_product":"p1","qty_on_hand":"` + qty + `"},` + shopOK + `}`
			}
			clerk := shopStep("confirm_order_as_clerk", shopConfirm, `{"order":{"id_order":"o2"},`+shopOK+`}`, "create_order")
			if changedItself {
				clerk = shopStep("confirm_order_as_clerk", shopConfirm, `{"order":{"id_order":"o2","n":"1"},`+shopOK+`}`, "create_order").failing("order.n", "2", "1")
			}
			return shopRecord(
				shopStep("create_product", shopCreate, product("10")),
				shopStep("create_order", shopOrder, `{"order":{"id_order":"o1"},`+shopOK+`}`, "create_product"),
				shopStep("confirm_order_short", shopConfirm, `{"status":{"code":"REJECTED","details":[{"app_code":1305,"reason":"Short"}]}}`, "create_order"),
				shopStep("get_product_after_confirm_order_short", shopGet, product("-1"), "create_product").failing("product.qty_on_hand", "10", "-1"),
				clerk.as("clerk"),
				shopStep("get_product_after_confirm_order_as_clerk", shopGet, product("6"), "create_product").failing("product.qty_on_hand", "8", "6"),
			)
		}
	}
	sameRecord := func() *runner.Record {
		return shopRecord(
			shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","total_minor":"7"},`+shopOK+`}`).failing("order.total_minor", "9", "7"),
			shopStep("cancel", shopCancel, `{"order":{"id_order":"o1","total_minor":"7","status":"ORDER_STATUS_CANCELLED"},`+shopOK+`}`, "create_order").failing("order.total_minor", "9", "7"),
			shopStep("replay", shopOrder, `{"order":{"id_order":"o1","total_minor":"7","status":"ORDER_STATUS_PENDING"},`+shopOK+`}`, "create_order").failing("order.status", "ORDER_STATUS_CANCELLED", "ORDER_STATUS_PENDING").failing("order.total_minor", "9", "7"),
		)
	}
	asBefore := func(readBetween bool, last recStep) func() *runner.Record {
		return func() *runner.Record {
			order := func(id, status string) string {
				return `{"order":{"id_order":"` + id + `","status":"` + status + `","lines":[{"id_product":"p1","qty":"2"}]}}`
			}
			steps := []recStep{
				shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","qty_on_hand":"0"}}`),
				shopStep("add_stock", shopAdd, `{"qty_on_hand":"10"}`, "create_product"),
				shopStep("create_order", shopOrder, order("o1", "PENDING"), "create_product"),
				shopStep("create_order_2", shopOrder, order("o2", "PENDING"), "create_product"),
			}
			if readBetween {
				steps = append(steps, shopStep("get_pending", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"10"}}`, "create_product"))
			}
			cancel := shopStep("cancel_2", shopCancel, order("o2", "CANCELLED"), "create_order_2").with(func(st *runner.StepRecord) { st.Request = json.RawMessage(`{"id_order":"o2"}`) })
			return shopRecord(append(steps, cancel, shopStep("confirm_order", shopConfirm, order("o1", "CONFIRMED"), "create_order"), last)...)
		}
	}
	asBeforeGet := shopStep("get", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"7"}}`, "create_product")
	asBeforeList := shopStep("list", shopList, `{"products":[{"id_product":"p0","qty_on_hand":"1"},{"id_product":"p1","qty_on_hand":"7"}]}`)
	sku := func(read string, want string, more ...recStep) func() *runner.Record {
		return func() *runner.Record {
			return shopRecord(append([]recStep{
				idStep("create", shopCreate, `{"product":{"id_product":"p1","sku":"SKU-A"}}`).held("product.sku", "SKU-A"),
				idStep("get", shopGet, `{"product":{"id_product":"p1","sku":"`+read+`"}}`, "create").failing("product.sku", want, read),
			}, more...)...)
		}
	}
	emptied := func() *runner.Record {
		get := func(id, response string) recStep {
			return shopStep(id, shopGet, response, "create_product").with(func(st *runner.StepRecord) { st.Request = []byte(`{"id_product":"p1"}`) })
		}
		return shopRecord(
			shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","sku":"s1"},`+shopOK+`}`),
			get("admin_get", `{"product":{"id_product":"p1","sku":"s1"},`+shopOK+`}`),
			get("clerk_get", `{"product":{"id_product":"","sku":""},`+shopOK+`}`).failing("product.sku", "s1", "").as("clerk"),
		)
	}
	for _, c := range []attrCase{
		{name: "one read answers a field its write returned otherwise and no other read settles it", env: "shop", rec: sku("sku-a", "SKU-A"), step: "get", path: "product.sku", kind: reasonUnclear, blamed: "create"},
		{name: "an empty value the read answers is shown", env: "shop", rec: sku("", "SKU-A"), step: "get", path: "product.sku", kind: reasonUnclear, blamed: "create"},
		{name: "without a reference the read agreed with, the contradiction stays on the write", env: "shop", rec: sku("sku-a", "SKU-B"), step: "get", path: "product.sku", kind: reasonStored, blamed: "create"},
		{name: "a later read after another write does not settle it", env: "shop", rec: sku("sku-a", "SKU-A",
			idStep("add", shopAdd, `{"qty_on_hand":"6"}`, "create"), idStep("list", shopList, `{"products":[{"id_product":"p1","sku":"SKU-A"}]}`)),
			step: "get", path: "product.sku", kind: reasonUnclear, blamed: "create"},
		{name: "two read rpcs agree against what the write answered", env: "shop", rec: sku("sku-a", "SKU-A",
			idStep("list", shopList, `{"products":[{"id_product":"p0","sku":"SKU-0"},{"id_product":"p1","sku":"sku-a"}]}`)),
			step: "get", path: "product.sku", kind: reasonStored, blamed: "create"},
		{name: "another read agrees with the write, so the disagreeing read is the suspect", env: "shop", rec: sku("sku-a", "SKU-A",
			idStep("list", shopList, `{"products":[{"id_product":"p1","sku":"SKU-A"}]}`)),
			step: "get", path: "product.sku", kind: reasonDiffers, blamed: ""},
		{name: "the write's own answer is not known to be unchanged", env: "shop", rec: func() *runner.Record {
			return shopRecord(idStep("create", shopCreate, `{"product":{"id_product":"p1","sku":"SKU-A"}}`),
				idStep("get", shopGet, `{"product":{"id_product":"p1","sku":"sku-a"}}`, "create").failing("product.sku", "SKU-A", "sku-a"))
		}, step: "get", path: "product.sku", kind: reasonWrite, blamed: "create"},
		{name: "the write carries no such field, so a wrong value after it stays on the write", env: "shop", rec: func() *runner.Record {
			return shopRecord(idStep("create", shopCreate, `{"product":{"id_product":"p1","sku":"SKU-A","price_minor":"5"}}`).held("product.sku", "SKU-A"),
				idStep("add", shopAdd, `{"qty_on_hand":"6"}`, "create"),
				idStep("get", shopGet, `{"product":{"id_product":"p1","sku":"SKU-A","price_minor":"6"}}`, "create").failing("product.price_minor", "5", "6"))
		}, step: "get", path: "product.price_minor", kind: reasonWrite, blamed: "add"},
		{name: "a list item the write answered is shown with its index", env: "shop", rec: func() *runner.Record {
			return shopRecord(idStep("create", shopCreate, `{"product":{"id_product":"p1"}}`),
				idStep("batch", shopBatch, `{"results":[{"id_product":"p1","qty_on_hand":"0"},{"id_product":"p1","qty_on_hand":"12"}]}`, "create").held("results.1.qty_on_hand", "12"),
				idStep("get", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"6"}}`, "create").failing("product.qty_on_hand", "12", "6"))
		}, step: "get", path: "product.qty_on_hand", kind: reasonUnclear, blamed: "batch"},
		{name: "a server error is the read's own and what reads it is its knock-on", env: "shop", rec: func() *runner.Record {
			return shopRecord(idStep("create", shopCreate, `{"product":{"id_product":"p1"}}`),
				idStep("get", shopGet, ``, "create").with(func(st *runner.StepRecord) {
					st.Status, st.HTTPStatus, st.Transport, st.Error = runner.StatusFailed, 500, &runner.TransportError{Code: "internal", Message: "pool exhausted"}, "internal: pool exhausted"
				}),
				idStep("get_again", shopGet, `{}`, "create").heldBy("get", "product.sku"))
		}, step: "get_again", path: "product.sku", kind: reasonKnockOn, blamed: ""},
		{name: "the same items in another order are the read's", env: "shop", rec: func() *runner.Record {
			return shopRecord(idStep("create", shopCreate, `{"product":{"id_product":"p1"}}`), idStep("create_2", shopCreate, `{"product":{"id_product":"p2"}}`),
				idStep("list", shopList, `{"products":[{"id_product":"p2"},{"id_product":"p1"}]}`, "create", "create_2").failing("products.0.id_product", "p1", "p2"))
		}, step: "list", path: "products.0.id_product", kind: reasonOrder, blamed: ""},
		{name: "a step unevaluated behind a failed write is filed under that write", env: "shop", rec: func() *runner.Record {
			return shopRecord(shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`).failing("product.sku", "A", nil),
				shopStep("add", shopAdd, `{"qty_on_hand":"1"}`, "create"),
				shopStep("get_after_add", shopGet, `{"product":{"id_product":"p1"}}`, "create").heldBy("create", "product.sku"))
		}, step: "get_after_add", path: "status", kind: reasonKnockOn, blamed: "create"},
		{name: "a step held back by a read that echoes the write names the write", env: "shop", rec: func() *runner.Record {
			return shopRecord(shopStep("create_order", shopOrder, order7).failing("order.total_minor", "9", "7"),
				shopStep("fetch_before", shopFetch, order7, "create_order").failing("order.total_minor", "9", "7"),
				shopStep("fetch_after", shopFetch, order7, "create_order").heldBy("fetch_before", "order.total_minor"))
		}, step: "fetch_after", path: "order.total_minor", kind: reasonKnockOn, blamed: "create_order"},
		{name: "a read held behind a held read names the write that lost the field", env: "shop", rec: func() *runner.Record {
			return shopRecord(shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`).failing("product.created_at", "t", nil),
				shopStep("get", shopGet, `{"product":{"id_product":"p1","created_at":"t"}}`, "create").heldBy("create", "product.created_at"),
				shopStep("get_as_clerk", shopGet, `{"product":{"id_product":"p1","created_at":"t"}}`, "create").heldBy("get", "product.created_at"))
		}, step: "get_as_clerk", path: "status", kind: reasonKnockOn, blamed: "create"},
		{name: "a read held back that answers what the read it copies answered is a knock-on", env: "shop", rec: clerkHeld("1249"),
			step: "get_as_clerk", path: "product.price_minor", kind: reasonKnockOn, blamed: "create"},
		{name: "a read held back that answers otherwise than the same read as another profile is filed under the read as its profile", env: "shop", rec: clerkHeld("0"),
			step: "get_as_clerk", path: "product.price_minor", kind: reasonProfile, blamed: "",
			check: func(r reason) bool { return r.Profile == "clerk" && r.Other == "default" }},
		{name: "a change the write itself answered is the write's: confirm", env: "shop", rec: fiveOrders, step: "confirm_order", path: "order.total_minor", kind: reasonWrite, blamed: "create_order"},
		{name: "a change the write itself answered is the write's: fetch", env: "shop", rec: fiveOrders, step: "fetch_after_confirm", path: "order.total_minor", kind: reasonWrite, blamed: "create_order"},
		{name: "a change the write itself answered is the write's: cancel", env: "shop", rec: fiveOrders, step: "cancel_order", path: "order.total_minor", kind: reasonWrite, blamed: "create_order"},
		{name: "a change the write itself answered is the write's: fetch after cancel", env: "shop", rec: fiveOrders, step: "fetch_order_after_cancel_order", path: "order.total_minor", kind: reasonWrite, blamed: "create_order"},
		{name: "a wrong level after a passing write that does not carry it stays on that write", env: "shop", rec: wrongLevel, step: "get_product_after_confirm_order", path: "product.price_minor", kind: reasonWrite, blamed: "confirm_order"},
		{name: "a wrong level after a later write still names the first", env: "shop", rec: wrongLevel, step: "get_product_after_cancel_order", path: "product.price_minor", kind: reasonWrite, blamed: "confirm_order"},
		{name: "a list answering other items after unchanged writes is the read's own", env: "shop", rec: otherItems(false), step: "list", path: "products.0.id_product", kind: reasonSet, blamed: ""},
		{name: "a write that failed before the list keeps the read from being blamed for its set", env: "shop", rec: otherItems(true), step: "list", path: "products.0.id_product", kind: "!" + reasonSet, blamed: "?"},
		{name: "changes after a write that failed at the transport are filed under it", env: "shop", rec: transport(), step: "get", path: "product.qty_on_hand", kind: reasonKnockOn, blamed: "add"},
		{name: "a refusal after a transport failure is filed under it", env: "shop", rec: transport(), step: "confirm_order", path: "status.code", kind: reasonKnockOn, blamed: "add"},
		{name: "another product after a transport failure is filed under it", env: "shop", rec: transport(), step: "get_2", path: "product.qty_on_hand", kind: reasonKnockOn, blamed: "add"},
		{name: "a field the failed write does not answer is not its", env: "shop", rec: transport(), step: "create_order", path: "order.lines.0.qty", kind: "!" + reasonKnockOn, blamed: "!add"},
		{name: "another record is not the failed write's", env: "shop", rec: transport(0, 1, 2, 6), step: "get_2", path: "product.qty_on_hand", kind: "!" + reasonKnockOn, blamed: "!add"},
		{name: "a write failing on another record is not blamed on an earlier write of the same rpc", env: "shop", rec: sameRPC, step: "add_as_other", path: "qty_on_hand", kind: reasonWrite, blamed: ""},
		{name: "a write on the same record echoes the earlier write", env: "shop", rec: sameRPC, step: "add_more_to_a", path: "qty_on_hand", kind: reasonWrite, blamed: "add_zero"},
		{name: "a write answering other than a later read of the record shows both values", env: "shop", rec: laterRead(`{"order":{"id_order":"o1","total_minor":"9"}}`, false), step: "confirm_order", path: "order.total_minor", kind: reasonStored, blamed: "",
			check: func(r reason) bool { return r.Want == "7" && r.Got == "9" && r.ReadRPC == "FetchOrder" }},
		{name: "an empty read is shown", env: "shop", rec: laterRead(`{"order":{"id_order":"o1","total_minor":""}}`, false), step: "confirm_order", path: "order.total_minor", kind: reasonStored, blamed: "",
			check: func(r reason) bool { return r.Got == "" && strings.Contains(r.String(), `but FetchOrder read ""`) }},
		{name: "a read after a later write of the record says nothing about what the first write stored", env: "shop", rec: laterRead(`{"order":{"id_order":"o1","total_minor":"9"}}`, true), step: "confirm_order", path: "order.total_minor", kind: "!" + reasonStored, blamed: "?"},
		{name: "an earlier write that changed the same field is the suspect of a later read", envelope: true, env: "shop", rec: earlierField, step: "get_product_after_confirm_order", path: "product.qty_on_hand", kind: reasonWrite, blamed: "add_stock"},
		{name: "an earlier write that changed the same field is the suspect of a refusal", envelope: true, env: "shop", rec: earlierField, step: "confirm_exact", path: "status.code", kind: reasonWrite, blamed: "add_stock"},
		{name: "an earlier write that changed the same field is the suspect of a later status", envelope: true, env: "shop", rec: earlierField, step: "fetch_order_after_confirm_exact", path: "order.status", kind: reasonWrite, blamed: "add_stock"},
		{name: "a stock write the refused rpc's contract moves is the suspect of its refusal", envelope: true, env: "effects", rec: earlierField, step: "confirm_exact", path: "status.code", kind: reasonWrite, blamed: "add_stock"},
		{name: "a refusal nothing the earlier changed write answered reaches is the write's own", envelope: true, env: "effects", rec: ownFlip(shopCreate, shopCancel, `{"id_order":"o1"}`), step: "flip", path: "status.code", kind: reasonWrite, blamed: ""},
		{name: "a refusal of a write that sends the changed value is filed under the earlier write", envelope: true, env: "effects", rec: ownFlip(shopCreate, shopCancel, `{"id_order":"o1","price_minor":"249"}`), step: "flip", path: "status.code", kind: reasonWrite, blamed: "create_product"},
		{name: "a refusal of a write whose contract needs the earlier rpc is filed under it", envelope: true, env: "effects", rec: ownFlip(shopAdd, shopConfirm, `{"id_order":"o1"}`), step: "flip", path: "status.code", kind: reasonWrite, blamed: "create_product"},
		{name: "without that need the same refusal is the write's own", envelope: true, env: "effects", rec: ownFlip(shopBatch, shopConfirm, `{"id_order":"o1"}`), step: "flip", path: "status.code", kind: reasonWrite, blamed: ""},
		{name: "a write answering its list in another order than the read says so", env: "shop", rec: listOrder(false), step: "create_order", path: "order.lines.0.qty", kind: reasonStoredOrder, blamed: "",
			check: func(r reason) bool { return r.Path == "order.lines" && r.ReadRPC == "FetchOrder" }},
		{name: "a read that moved too says nothing about the order the write stored", env: "shop", rec: listOrder(true), step: "create_order", path: "order.lines.0.qty", kind: "!" + reasonStoredOrder, blamed: "?"},
		{name: "an envelope is never a stored field", envelope: true, env: "shop", rec: envelopeBatch, step: "batch", path: "status.code", kind: "!" + reasonStored, blamed: "?"},
		{name: "an item envelope is never a stored field", envelope: true, env: "shop", rec: envelopeBatch, step: "batch", path: "results.0.status.code", kind: "!" + reasonStored, blamed: "?"},
		{name: "without a reference the nearest write is named", envelope: true, env: "shop", rec: func() *runner.Record { return replaysRecord(true) }, step: "get_product", path: "product.qty_on_hand", kind: reasonWrite, blamed: "confirm_order"},
		{name: "against a reference a write refused as before is no candidate", envelope: true, env: "shop", moved: changed("get_product", "product.qty_on_hand", "15", "14"), rec: func() *runner.Record { return replaysRecord(true) }, step: "get_product", path: "product.qty_on_hand", kind: reasonUnclear, blamed: "create_order"},
		{name: "a refused write whose contract effects move the field stays the first candidate", envelope: true, env: "effects", moved: changed("get_product", "product.qty_on_hand", "15", "14"), rec: func() *runner.Record { return replaysRecord(true) }, step: "get_product", path: "product.qty_on_hand", kind: reasonUnclear, blamed: "confirm_order"},
		{name: "a total that recomputes from a changed price is filed under the price write", envelope: false, env: "shop", rec: priceTotal("3498"), step: "create_order", path: "order.total_minor", kind: reasonWrite, blamed: "create_product"},
		{name: "a read of a total that recomputes from a changed price is filed under the price write", env: "shop", rec: priceTotal("3498"), step: "fetch_order", path: "order.total_minor", kind: reasonWrite, blamed: "create_product"},
		{name: "a total the prices do not give is the order write's own", env: "shop", rec: priceTotal("3497"), step: "create_order", path: "order.total_minor", kind: reasonWrite, blamed: ""},
		{name: "a read answering what the order write answered stays on the order write", env: "shop", rec: priceTotal("3497"), step: "fetch_order", path: "order.total_minor", kind: reasonWrite, blamed: "create_order"},
		{name: "a list gaining items is not a knock-on of a changed value", envelope: true, env: "shop", rec: func() *runner.Record {
			list := shopStep("list", shopList, `{"products":[{"id_product":"p9"},{"id_product":"p1"}],`+shopOK+`}`).failing("products.1", false, true)
			list.Expect[0].Rule = "exists"
			return shopRecord(shopStep("create", shopCreate, `{"product":{"id_product":"p1","price_minor":"249"},`+shopOK+`}`).failing("product.price_minor", "250", "249"), list)
		}, step: "list", path: "products.1", kind: reasonSet, blamed: ""},
		{name: "a refusal after value changes is the read's own and names the profile", envelope: true, rec: refusedAfter(false), step: "fetch_as_clerk", path: "status.code", kind: reasonRefused, blamed: "",
			check: func(r reason) bool {
				return r.Profile == "clerk" && r.Other == "default" && r.Got == "1302 OrderNotFound"
			}},
		{name: "a refusal after a refused write stays on that write", envelope: true, env: "shop", rec: refusedAfter(true), step: "fetch_as_clerk", path: "status.code", kind: reasonWrite, blamed: "create_order"},
		{name: "a knock-on needs the value the earlier write answered", rec: knockValue, step: "list", path: "products.0.price_minor", kind: reasonKnockOn, blamed: "create"},
		{name: "another value is not explained by the write", rec: knockValue, step: "list_2", path: "products.0.price_minor", kind: "", blamed: "", check: func(r reason) bool { return !r.blames() }},
		{name: "a streamed message carrying the value a write answered is filed under the write", env: "shop", rec: func() *runner.Record {
			order := `{"order":{"id_order":"o1","total_minor":"1750"}}`
			return shopRecord(shopStep("create_order", shopOrder, order).failing("order.total_minor", "4250", "1750"),
				shopStep("watch_order", shopWatch, `{"messages":[`+order+`]}`, "create_order").failing("messages.0.order.total_minor", "4250", "1750"))
		}, step: "watch_order", path: "messages.0.order.total_minor", kind: reasonWrite, blamed: "create_order"},
		{name: "a step held back by a read is never filed under that read as a write", env: "shop", rec: heldRead, step: "get_again", path: "product.qty_on_hand", kind: reasonKnockOn, blamed: "",
			check: func(r reason) bool {
				return r.Read == "get" && r.String() == "knock-on of read get (ProductService/GetProduct)"
			}},
		{name: "a step held back by a read waits on the write the read observes", env: "effects", moved: changed("get", "product.qty_on_hand", "8", "7"), rec: heldReadAfterConfirm, step: "get_again", path: "product.qty_on_hand", kind: reasonKnockOn, blamed: "confirm_order"},
		{name: "an auth probe without a token failing its own transport expectation is its own suspect", env: "shop", rec: probe(runner.NoAuthProfile), step: "watch_without_token", path: "transport.code", kind: reasonProbe, blamed: ""},
		{name: "an auth probe with an invalid token failing its own transport expectation is its own suspect", env: "shop", rec: probe(chain.InvalidTokenAuth), step: "watch_without_token", path: "transport.code", kind: reasonProbe, blamed: ""},
		{name: "a refusal following a batch whose verdict moved is filed under that batch", envelope: true, env: "effects", pinned: true, rec: confirmAfter(movedBatch()), step: "confirm", path: "status.code", kind: reasonWrite, blamed: "batch"},
		{name: "a changed status following a batch whose verdict moved is filed under that batch", envelope: true, env: "effects", pinned: true, rec: confirmAfter(movedBatch()), step: "confirm", path: "order.status", kind: reasonWrite, blamed: "batch"},
		{name: "a refusal after a read of the stock it needs changed after the batch is filed under the batch", envelope: true, env: "effects", pinned: true,
			rec:  confirmAfter(answeredBatch(), shopStep("get_a", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"4"},`+shopOK+`}`, "create_a").failing("product.qty_on_hand", "7", "4")),
			step: "confirm", path: "status.code", kind: reasonWrite, blamed: "batch"},
		{name: "a changed field the refused write's contract does not move explains nothing", envelope: true, env: "effects", pinned: true,
			rec:  confirmAfter(answeredBatch(), shopStep("get_a", shopGet, `{"product":{"id_product":"p1","name":"x"},`+shopOK+`}`, "create_a").failing("product.name", "X", "x")),
			step: "confirm", path: "status.code", kind: reasonWrite, blamed: "", check: func(r reason) bool { return r.blamed("confirm") == "" }},
		{name: "a later write on a record an earlier changed write touched folds into that write", envelope: true, env: "shop", rec: laterWrite, step: "add_large", path: "qty_on_hand", kind: reasonWrite, blamed: "add_negative"},
		{name: "a second later write on that record folds into it too", envelope: true, env: "shop", rec: laterWrite, step: "add_larger", path: "qty_on_hand", kind: reasonWrite, blamed: "add_negative"},
		{name: "a write on another record stays its own", envelope: true, env: "shop", rec: laterWrite, step: "add_other", path: "qty_on_hand", kind: reasonWrite, blamed: ""},
		{name: "a later write changing another field of the record keeps its own blame", envelope: true, env: "shop", rec: sameRecord, step: "replay", path: "order.status", kind: reasonWrite, blamed: "!cancel"},
		{name: "the suspect write is named without a profile", envelope: true, rec: profileWrite(false), step: "get_product_after_confirm_order_short", path: "product.qty_on_hand", kind: reasonWrite, blamed: "confirm_order_short",
			check: func(r reason) bool { return r.Profile == "" }},
		{name: "the suspect write carries its profile", envelope: true, rec: profileWrite(false), step: "get_product_after_confirm_order_as_clerk", path: "product.qty_on_hand", kind: reasonWrite, blamed: "confirm_order_as_clerk",
			check: func(r reason) bool { return r.Profile == "clerk" }},
		{name: "a write that changed itself stays the suspect with its profile", envelope: true, rec: profileWrite(true), step: "get_product_after_confirm_order_as_clerk", path: "product.qty_on_hand", kind: reasonWrite, blamed: "confirm_order_as_clerk",
			check: func(r reason) bool { return r.Profile == "clerk" }},
		{name: "a read that differs only under another profile is filed under the read as that profile", envelope: true, env: "effects", moved: changed("clerk_get", "product.price_minor", "500", "0"),
			rec: func() *runner.Record {
				return principalRecord(shopStep("fetch", shopFetch, `{"order":{"id_order":"o1"},`+shopOK+`}`))
			},
			step: "clerk_get", path: "product.price_minor", kind: reasonProfile, blamed: "",
			check: func(r reason) bool {
				return r.Profile == "clerk" && r.Other == "default" && r.blamed("clerk_get") == ""
			}},
		{name: "a changed write between the two reads leaves the profile unproven", envelope: true, env: "effects",
			moved: append(changed("clerk_get", "product.price_minor", "500", "0"), diff.Change{Step: "create_order", Path: "order.id_order", Kind: diff.KindChanged, Want: "o0", Got: "o1"}),
			rec: func() *runner.Record {
				return principalRecord(shopStep("create_order", shopOrder, `{"order":{"id_order":"o1"},`+shopOK+`}`, "create_product"))
			},
			step: "clerk_get", path: "product.price_minor", kind: "!" + reasonProfile, blamed: "?"},
		{name: "a write that neither answers nor moves the field is no candidate", envelope: true, env: "effects", moved: changed("clerk_get", "product.price_minor", "500", "0"),
			rec: func() *runner.Record {
				rec := principalRecord(shopStep("fetch", shopFetch, `{"order":{"id_order":"o1"},`+shopOK+`}`))
				rec.Steps = rec.Steps[:4]
				return rec
			}, step: "clerk_get", path: "product.price_minor", kind: reasonUnclear, blamed: "create_product", check: func(r reason) bool { return r.blames() }},
		{name: "a list grown by a changed write is filed under that write", envelope: true, env: "effects",
			moved: []diff.Change{{Step: "replay", Path: "order.id_order", Kind: diff.KindChanged, Want: "o1", Got: "o2"}, {Step: "list_orders", Path: "orders", Kind: diff.KindLength, Want: 1, Got: 2}},
			rec: func() *runner.Record {
				return shopRecord(shopStep("create_order", shopOrder, `{"order":{"id_order":"o1"},`+shopOK+`}`),
					shopStep("replay", shopOrder, `{"order":{"id_order":"o2"},`+shopOK+`}`),
					shopStep("list_orders", "shop.orders.v1.OrderService/ListOrders", `{"orders":[{"id_order":"o1"},{"id_order":"o2"}],`+shopOK+`}`))
			}, step: "list_orders", path: "orders", kind: reasonWrite, blamed: "replay"},
		{name: "a record emptied under another profile is filed under the read as that profile", envelope: true, env: "effects", moved: changed("clerk_get", "product.sku", "s1", ""),
			rec: emptied, step: "clerk_get", path: "product.sku", kind: reasonProfile, blamed: "", check: func(r reason) bool { return r.Profile == "clerk" && r.blamed("clerk_get") == "" }},
		{name: "a read after the other movers is unclear between the write that answered as before and the read", env: "effects", moved: changed("get", "product.qty_on_hand", "8", "7"),
			rec: asBefore(true, asBeforeGet), step: "get", path: "product.qty_on_hand", kind: reasonUnclear, blamed: "confirm_order", check: func(r reason) bool { return r.Read == "get" }},
		{name: "an item of a list is linked to the writes naming its id", env: "effects", moved: changed("list", "products.1.qty_on_hand", "8", "7"),
			rec: asBefore(true, asBeforeList), step: "list", path: "products.1.qty_on_hand", kind: reasonUnclear, blamed: "confirm_order", check: func(r reason) bool { return r.Read == "list" }},
		{name: "with no read in between the write that answered as before is still unclear", env: "effects", moved: changed("get", "product.qty_on_hand", "8", "7"),
			rec: asBefore(false, asBeforeGet), step: "get", path: "product.qty_on_hand", kind: reasonUnclear, blamed: "confirm_order"},
		{name: "with no read in between a list item is still unclear", env: "effects", moved: changed("list", "products.1.qty_on_hand", "8", "7"),
			rec: asBefore(false, asBeforeList), step: "list", path: "products.1.qty_on_hand", kind: reasonUnclear, blamed: "confirm_order"},
		{name: "a read no longer refused is its own suspect", envelope: true, rec: func() *runner.Record {
			return shopRecord(shopStep("get_unknown", "shop.customers.v1.CustomerService/GetCustomer", `{"customer":{"name":""},"status":{"code":"SUCCESS"}}`).failing("status.code", "REJECTED", "SUCCESS"))
		}, step: "get_unknown", path: "customer", kind: reasonCode, blamed: "", check: func(r reason) bool { return r.Want == "REJECTED" && r.Got == "SUCCESS" }},
		{name: "a hedged read is filed under the write that heads the hedge", env: "effects", moved: changed("get", "product.qty_on_hand", "8", "9"), rec: func() *runner.Record {
			order := `{"order":{"id_order":"o1","status":"CONFIRMED","lines":[{"id_product":"p1","qty":"2"}]}}`
			return shopRecord(shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","qty_on_hand":"0"}}`),
				shopStep("add_stock", shopAdd, `{"qty_on_hand":"10"}`, "create_product"),
				shopStep("create_order", shopOrder, order, "create_product"),
				shopStep("confirm_order", shopConfirm, order, "create_order"),
				shopStep("get", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"9"}}`, "create_product"))
		}, step: "get", path: "product.qty_on_hand", kind: reasonUnclear, blamed: "confirm_order", check: func(r reason) bool { return shortRPC(r.rpc(shopGet)) == "OrderService/ConfirmOrder" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.envelope {
				chain.SetEnvelope("status.code", "SUCCESS")
				defer chain.SetEnvelope("", "")
			}
			var e *env
			switch c.env {
			case "shop":
				e = &env{cat: catalogtest.Shop()}
			case "effects":
				e = effectsEnv(t)
			}
			rec := c.rec()
			a := runAttribution(e, rec)
			switch {
			case c.pinned:
				a = pinnedAttribution(e, rec, nil)
			case c.moved != nil:
				a = changesAttribution(e, rec, c.moved)
			}
			r := a.of(c.step, c.path)
			if !is(c.kind, r.Kind) || !is(c.blamed, r.blamed(c.step)) || c.check != nil && !c.check(r) {
				t.Errorf("got %+v, want kind %q blaming %q", r, c.kind, c.blamed)
			}
		})
	}
}

func TestASuspectWriteIsTheWriteAReadObserves(t *testing.T) {
	step := func(id, call string, refs ...string) *runner.StepRecord {
		st := &runner.StepRecord{ID: id, Call: "x.v1.S/" + call, BodyRefs: map[string]string{}}
		for i, r := range refs {
			st.BodyRefs[string(rune('a'+i))] = "${" + r + ".id}"
		}
		return st
	}
	rec := &runner.Record{Steps: []*runner.StepRecord{
		step("create_item", "CreateItem"),
		step("create_box", "CreateBox", "create_item"),
		step("move_box", "MoveBox", "create_box"),
		step("fill_item", "FillItem", "create_item"),
		step("get_item", "GetItem", "create_item"),
		step("get_item_after_move_box", "GetItem", "create_item"),
		step("list_items", "ListItems"),
		{ID: "get_item_unknown_id", Call: "x.v1.S/GetItem", BodyRefs: map[string]string{"id_item": "${create_item.id}-unknown"}},
	}}
	for _, c := range []struct {
		read, bad, want string
		knock           bool
	}{
		{"get_item", "", "fill_item", false},
		{"get_item", "move_box", "move_box", false},
		{"get_item_after_move_box", "fill_item", "fill_item", false},
		{"get_item_after_move_box", "move_box", "move_box", false},
		{"list_items", "create_box", "create_box", true},
		{"list_items", "", "", false},
		{"fill_item", "", "", false},
		{"get_item_unknown_id", "", "", false},
	} {
		i, knock := suspectWrite(rec, c.read, "", map[string]bool{c.bad: c.bad != ""})
		got := ""
		if i >= 0 {
			got = rec.Steps[i].ID
		}
		if got != c.want || knock != c.knock {
			t.Errorf("%s with %q failing: got %q knock-on %v, want %q %v", c.read, c.bad, got, knock, c.want, c.knock)
		}
	}
	for _, orderRef := range []string{"${id_order}", "${exports.id_order}"} {
		exported := func(id, call, name string, refs map[string]string) *runner.StepRecord {
			st := &runner.StepRecord{ID: id, Call: "shop.v1.S/" + call, BodyRefs: refs}
			if name != "" {
				st.Exported = map[string]any{name: id + "-1"}
			}
			return st
		}
		rec := &runner.Record{Steps: []*runner.StepRecord{
			exported("create_product_a", "CreateProduct", "id_a", nil),
			exported("add_stock_a", "AddStock", "", map[string]string{"id_product": "${id_a}"}),
			exported("create_order", "CreateOrder", "id_order", map[string]string{"lines.0.id_product": "${id_a}"}),
			exported("confirm_order", "ConfirmOrder", "", map[string]string{"id_order": orderRef}),
			exported("fetch_order", "FetchOrder", "", map[string]string{"id_order": orderRef}),
			exported("stock_a_confirmed", "GetProduct", "", map[string]string{"id_product": "${id_a}"}),
		}}
		if i, knock := suspectWrite(rec, "stock_a_confirmed", "", map[string]bool{}); i < 0 || rec.Steps[i].ID != "confirm_order" || knock {
			t.Errorf("%s: got step %d knock-on %v, want confirm_order found through the exported id", orderRef, i, knock)
		}
	}
}

func TestAttributionHelpers(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	for _, firstRefused := range []bool{false, true} {
		rec := replaysRecord(firstRefused)
		pos := map[string]int{}
		for i, st := range rec.Steps {
			pos[st.ID] = i
		}
		if got := entityWrites(rec, len(rec.Steps)-1, "", map[string]bool{}, pos); !slices.Equal(got, []int{2, 1, 0}) {
			t.Errorf("entityWrites (first refused %v): got steps %v, want the writes before the read without the repeats", firstRefused, got)
		}
	}
	var cancel, list, one any
	_ = json.Unmarshal([]byte(`{"order":{"id_order":"o2","id_customer":"c1","status":"CANCELLED"}}`), &cancel)
	_ = json.Unmarshal([]byte(`{"orders":[{"id_order":"o3","id_customer":"c1","status":"CONFIRMED"},{"id_order":"o2","id_customer":"c1","status":"CANCELLED"}]}`), &list)
	_ = json.Unmarshal([]byte(`{"orders":[{"id_order":"o3","id_customer":"c1","status":"CONFIRMED"}]}`), &one)
	for _, c := range []struct {
		read any
		path string
		same bool
	}{{list, "orders.0.status", false}, {list, "orders.1.status", true}, {one, "orders.0.status", false}} {
		if got := sameEntity(c.read, c.path, cancel, "order.status"); got != c.same {
			t.Errorf("sameEntity %s: got %v, want %v", c.path, got, c.same)
		}
	}
	for path, want := range map[string]string{"order.lines.0.qty": "order.lines", "orders.0.lines.1.qty": "orders[].lines", "messages.0.order.lines.0.id": "messages[].order.lines"} {
		if got := listOf(path); got != want {
			t.Errorf("listOf %s: got %q, want %q", path, got, want)
		}
	}
	const login, create = "shrt.test.v1.AuthService/Login", "shrt.test.v1.ThingService/Create"
	e := &env{cat: catalogtest.New(), cfg: &config.Config{Auth: &config.Auth{Call: login}}}
	for _, c := range []struct{ call, profile, want string }{
		{login, runner.NoAuthProfile, ""},
		{create, runner.NoAuthProfile, "as none"},
		{create, "clerk", "as clerk"},
		{create, "", ""},
	} {
		if got := asOf(e, &runner.StepRecord{Call: c.call, AuthProfile: c.profile}); got != c.want {
			t.Errorf("asOf %s %q: got %q, want %q", c.call, c.profile, got, c.want)
		}
	}
	const get = "x.v1.S/Get"
	refused := reason{Kind: reasonRefused, Step: "get", RPC: get, Got: "1102"}
	a := gateItem{Step: "get", Call: get, Path: "status.code", Reason: refused}
	b := gateItem{Step: "get", Call: get, Path: "customer.name", Reason: refused}
	w := gateItem{Step: "get", Call: get, Path: "customer.name", Reason: reason{Kind: reasonWrite, Step: "create", RPC: "x.v1.S/Create"}}
	if a.root() != b.root() || a.root() == w.root() || w.root() != "S/Create name" {
		t.Errorf("a read's own fault is one root whatever path shows it: got %q, %q, %q", a.root(), b.root(), w.root())
	}
}

func replaysRecord(firstRefused bool) *runner.Record {
	order := `{"order":{"id_order":"o1"},` + shopOK + `}`
	confirm := shopStep("confirm_order", shopConfirm, order, "create_order")
	if firstRefused {
		confirm = shopStep("confirm_order", shopConfirm, `{"status":{"code":"REJECTED","details":[{"app_code":1305}]}}`, "create_order")
	}
	steps := []recStep{
		shopStep("create_product", shopCreate, `{"product":{"id_product":"p1"},`+shopOK+`}`),
		shopStep("create_order", shopOrder, order, "create_product"),
		confirm,
		shopStep("replay_order", shopOrder, order, "create_product").with(func(st *runner.StepRecord) {
			st.BodyRefs["idempotency_key"] = "${steps.create_order.request.idempotency_key}"
		}),
		shopStep("confirm_again", shopConfirm, `{"status":{"code":"REJECTED","details":[{"app_code":1303}]}}`, "create_order"),
		shopStep("get_product", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"14"},`+shopOK+`}`, "create_product").failing("product.qty_on_hand", "15", "14"),
	}
	if firstRefused {
		steps = append(steps[:4], steps[5])
	}
	return shopRecord(steps...)
}
