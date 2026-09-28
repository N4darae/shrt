package chain_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
)

type sliceCase struct {
	name     string
	c        *chain.Chain
	target   string
	opts     chain.SliceOptions
	kept     string
	has      []string
	lacks    []string
	unmet    string
	err      []string
	envelope bool
	check    func(*testing.T, *chain.SliceResult)
}

func keptIDs(res *chain.SliceResult) []string {
	out := make([]string, 0, len(res.Kept))
	for _, k := range res.Kept {
		out = append(out, k.ID)
	}
	return out
}

func keptByID(res *chain.SliceResult) map[string]chain.Keep {
	kept := map[string]chain.Keep{}
	for _, k := range res.Kept {
		kept[k.ID] = k
	}
	return kept
}

func edges(from string, want ...chain.Prereq) func(string) []chain.Prereq {
	return func(rpc string) []chain.Prereq {
		if rpc == from {
			return want
		}
		return nil
	}
}

func refusedIn(ids map[string]string) func(string) (string, bool) {
	return func(id string) (string, bool) {
		why, ok := ids[id]
		return why, ok
	}
}

func steps(name string, s ...*chain.Step) *chain.Chain { return &chain.Chain{Name: name, Steps: s} }

func st(id, call string, body map[string]any, expect ...chain.Expectation) *chain.Step {
	return &chain.Step{ID: id, Call: call, Body: body, Expect: expect}
}

func ref(r string) map[string]any { return map[string]any{"id": r} }

func sliceFixture() *chain.Chain {
	c := steps("fixture",
		st("create_book", "BookService/CreateBook", map[string]any{"code": "B-${vars.tag}"}),
		st("set_limit", "LimitActionService/SetCounterpartyCreditLimit", map[string]any{"amount": "10"}),
		st("create_deal", "DealService/CreateDeal", map[string]any{"id_book": "${book}"}),
		st("noise", "DealService/CreateDeal", map[string]any{"id_book": "${create_book.id_book}"}),
		st("fetch_deal", "DealService/FetchDeal", map[string]any{"id_deal": "${create_deal.results.0.id_deal}"}, chain.Expectation{Path: "id_book", Equals: "${exports.book}"}),
	)
	c.Vars = map[string]any{"tag": "T1", "unused": "U"}
	c.Steps[0].Export = map[string]string{"book": "id_book"}
	return c
}

func orderFlow() *chain.Chain {
	return steps("order-flow",
		st("add_stock_a", "StockService/AddStock", nil),
		st("add_stock_b", "StockService/AddStock", nil),
		st("create_order", "OrderService/CreateOrder", nil),
		st("confirm_order", "OrderService/ConfirmOrder", map[string]any{"id_order": "${create_order.order.id_order}"}),
		st("cancel_order", "OrderService/CancelOrder", nil))
}

var aliasPrereqs = edges("OrderService/ConfirmOrder",
	chain.Prereq{RPC: "StockService/AddStock", Alias: "a", Edge: "needs"}, chain.Prereq{RPC: "StockService/AddStock", Alias: "b", Edge: "needs"})

func keepFixture() *chain.Chain {
	return steps("keepy",
		st("seed", "pkg.Svc/Create", nil),
		st("owner", "pkg.Svc/Create", nil),
		st("noise", "pkg.Svc/Update", map[string]any{"owner": "${owner.id}"}),
		st("boom", "pkg.Svc/Approve", ref("${seed.id}")),
		st("after", "pkg.Svc/Create", nil))
}

func line(product, qty string) map[string]any {
	return map[string]any{"id_product": "${" + product + ".product.id_product}", "qty": qty}
}

func productsAndStock(qty ...string) []*chain.Step {
	out := []*chain.Step{}
	for i, suffix := range []string{"", "_2", "_3"} {
		out = append(out, st("create_product"+suffix, "ProductService/CreateProduct", map[string]any{"sku": string(rune('a' + i))}))
	}
	for i, suffix := range []string{"", "_2", "_3"} {
		out = append(out, st("add_stock"+suffix, "StockService/AddStock", map[string]any{"id_product": "${create_product" + suffix + ".product.id_product}", "qty": qty[i]}))
	}
	return append(out, st("create_customer", "CustomerService/CreateCustomer", map[string]any{"email": "c@example.test"}))
}

func order(id string, lines ...any) *chain.Step {
	return st(id, "OrderService/CreateOrder", map[string]any{"id_customer": "${create_customer.customer.id_customer}", "lines": lines})
}

func confirm(id, orderID string) *chain.Step {
	return st(id, "OrderService/ConfirmOrder", map[string]any{"id_order": "${" + orderID + ".order.id_order}"})
}

func confirmShortageChain() *chain.Chain {
	c := steps("orders-confirm", productsAndStock("10", "11", "1")...)
	c.Steps = append(c.Steps,
		order("create_order", line("create_product", "3"), line("create_product_2", "4")),
		confirm("confirm_order", "create_order"),
		order("create_order_3", line("create_product_3", "1")),
		confirm("confirm_order_3", "create_order_3"),
		order("create_order_other", line("create_product", "1")),
		order("create_order_last_item", line("create_product", "3"), line("create_product_2", "100000")),
		confirm("confirm_order_last_item", "create_order_last_item"))
	return c
}

func literalProducerFixture() *chain.Chain {
	return steps("slice-repro",
		st("good_customer", "CustomerService/CreateCustomer", map[string]any{"email": "a@example.test"}, chain.Expectation{Path: "status.code", Equals: "SUCCESS"}),
		st("bad_customer", "CustomerService/CreateCustomer", map[string]any{"email": "no-at-sign"}, chain.Expectation{Path: "transport.code", Equals: "invalid_argument"}),
		st("list_literal", "OrderService/ListOrders", map[string]any{"id_customer": "cus-literal"}),
		st("list_ref", "OrderService/ListOrders", map[string]any{"id_customer": "${vars.who}"}),
		st("list_unset", "OrderService/ListOrders", nil))
}

var literalPrereqs = edges("OrderService/ListOrders", chain.Prereq{RPC: "CustomerService/CreateCustomer", Edge: "from", Field: "id_customer"})

func sameEmailChain() *chain.Chain {
	ok := chain.Expectation{Path: "status.code", Equals: "SUCCESS"}
	taken := chain.Expectation{Path: "status.details.0.reason", Equals: "EmailTaken"}
	const create = "shop.v1.CustomerService/CreateCustomer"
	c := steps("customers",
		st("create_customer", create, map[string]any{"email": "cus-${vars.tag}-a@example.test", "name": "Customer ${vars.tag}"}, ok),
		st("create_customer_same_email", create, map[string]any{"email": "${steps.create_customer.request.email}"}, taken),
		st("create_customer_other", create, map[string]any{"email": "cus-${vars.tag}-b@example.test", "name": "Customer ${vars.tag}"}, ok),
		st("create_customer_fixed", create, map[string]any{"email": "CUS-A@EXAMPLE.TEST"}, ok),
		st("create_customer_same_email_case", create, map[string]any{"email": "CUS-${vars.tag}-A@EXAMPLE.TEST", "name": "Customer ${vars.tag}"}, taken))
	c.Vars = map[string]any{"tag": "t"}
	return c
}

func cancelRestockChain() *chain.Chain {
	ok := chain.Expectation{Path: "status.code", Equals: "SUCCESS"}
	return steps("cancel-restock",
		st("create_product", "shop.v1.ProductService/CreateProduct", map[string]any{"sku": "a"}, ok),
		st("create_product_2", "shop.v1.ProductService/CreateProduct", map[string]any{"sku": "b"}, ok),
		st("add_stock", "shop.v1.StockService/AddStock", map[string]any{"id_product": "${create_product.product.id_product}"}, ok),
		st("add_stock_2", "shop.v1.StockService/AddStock", map[string]any{"id_product": "${create_product_2.product.id_product}"}, ok),
		st("create_customer", "shop.v1.CustomerService/CreateCustomer", nil, ok),
		st("create_other_customer", "shop.v1.CustomerService/CreateCustomer", nil, ok),
		st("create_order", "shop.v1.OrderService/CreateOrder", map[string]any{"id_customer": "${create_customer.customer.id_customer}",
			"lines": []any{map[string]any{"id_product": "${create_product.product.id_product}"}, map[string]any{"id_product": "${create_product_2.product.id_product}"}}}, ok),
		st("confirm_order", "shop.v1.OrderService/ConfirmOrder", map[string]any{"id_order": "${create_order.order.id_order}"}, ok),
		st("cancel_order", "shop.v1.OrderService/CancelOrder", map[string]any{"id_order": "${confirm_order.order.id_order}"}, ok),
		st("get_product_2_after_cancel", "shop.v1.ProductService/GetProduct", map[string]any{"id_product": "${create_product_2.product.id_product}"},
			chain.Expectation{Path: "product.qty_on_hand", Equals: "${add_stock_2.qty_on_hand}"}))
}

func batchLevelChain() *chain.Chain {
	line := func(p, qty string) map[string]any {
		return map[string]any{"id_product": "${" + p + ".product.id_product}", "qty": qty}
	}
	return steps("batch-level",
		st("create_a", "shop.v1.ProductService/CreateProduct", map[string]any{"sku": "a"}),
		st("create_b", "shop.v1.ProductService/CreateProduct", map[string]any{"sku": "b"}),
		st("batch_1", "shop.v1.StockService/AddStockBatch", map[string]any{"lines": []any{line("create_a", "4")}}),
		st("batch_3", "shop.v1.StockService/AddStockBatch", map[string]any{"lines": []any{line("create_a", "4")}}),
		st("batch_12", "shop.v1.StockService/AddStockBatch", map[string]any{"lines": []any{line("create_a", "4"), line("create_b", "5")}},
			chain.Expectation{Path: "results.0.qty_on_hand", Equals: 12}),
		st("get_b", "shop.v1.ProductService/GetProduct", map[string]any{"id_product": "${create_b.product.id_product}"},
			chain.Expectation{Path: "product.qty_on_hand", Equals: 5}))
}

func productChainReading(name string) *chain.Chain {
	return steps("stock",
		st("create_product_first", "ProductService/CreateProduct", map[string]any{"sku": "${vars." + name + "}-A"}),
		st("create_product_second", "ProductService/CreateProduct", map[string]any{"sku": "${vars." + name + "}-B"}),
		st("add_stock_second", "StockService/AddStock", map[string]any{"id_product": "${create_product_second.product.id_product}"}))
}

func keptRedChain() *chain.Chain {
	got := "SUCCESS"
	c := steps("kr",
		st("create", "S/CreateThing", map[string]any{"name": "x"}, chain.Expectation{Path: "status.code", Equals: "SUCCESS"}),
		st("confirm", "S/ConfirmThing", ref("${create.thing.id}"), chain.Expectation{Path: "status.code", Equals: "REJECTED"}, chain.Expectation{Path: "status.details.0.reason", Equals: "Short"}),
		st("later", "S/GetThing", ref("${create.thing.id}"), chain.Expectation{Path: "thing.qty", Equals: 0}))
	c.KeptRed = []chain.Pin{{Step: "confirm", Path: "status.code", Got: &got}, {Step: "confirm", Path: "status.details.0.reason"}, {Step: "later", Path: "thing.qty"}}
	return c
}

func pinnedLater() *chain.Chain {
	return steps("kr",
		st("create", "S/CreateThing", map[string]any{"name": "x"}, chain.Expectation{Path: "status.code", Equals: "SUCCESS"}),
		st("unrelated", "S/GetOther", ref("o-1"), chain.Expectation{Path: "status.code", Equals: "SUCCESS"}),
		st("confirm", "S/ConfirmThing", ref("${create.thing.id}"), chain.Expectation{Path: "status.code", Equals: "REJECTED"}),
		st("fetch_after_confirm", "S/GetThing", ref("${create.thing.id}"), chain.Expectation{Path: "thing.status", Equals: "PENDING"}))
}

func TestSlice(t *testing.T) {
	ok := chain.Expectation{Path: "status.code", Equals: "SUCCESS"}
	listFilter := steps("catalog-listproducts",
		st("create_product", "shop.v1.ProductService/CreateProduct", map[string]any{"sku": "sku-${vars.tag}-c"}, ok),
		st("create_product_prefix_case", "shop.v1.ProductService/CreateProduct", map[string]any{"sku": "SKU-${vars.tag}-case"}, ok),
		st("create_customer", "shop.v1.CustomerService/CreateCustomer", map[string]any{"name": "fixed"}, ok),
		st("list_products", "shop.v1.ProductService/ListProducts", map[string]any{"sku_prefix": "sku-${vars.tag}-"},
			chain.Expectation{Path: "products.0.id_product", Equals: "${create_product.product.id_product}"}, chain.Expectation{Path: "products.1", Exists: new(false)}),
		&chain.Step{ID: "list_products_without_token", Call: "shop.v1.ProductService/ListProducts", Body: map[string]any{"sku_prefix": "sku-${vars.tag}-"},
			SkipAuth: true, Expect: []chain.Expectation{{Path: "transport.code", Equals: "unauthenticated"}}})
	listFilter.Vars = map[string]any{"tag": "t"}
	full := literalProducerFixture()
	withRun := literalProducerFixture()
	withRun.Steps[1].Expect = nil
	refusedDrop := steps("orders",
		st("create_order", "OrderService/CreateOrder", nil),
		st("create_order_no_lines", "OrderService/CreateOrder", nil),
		st("confirm_twice", "OrderService/ConfirmOrder", nil),
		st("cancel_order", "OrderService/CancelOrder", ref("${create_order.id}")))
	withConfirmRefused := confirmShortageChain()
	withConfirmRefused.Steps[8].Expect = []chain.Expectation{{Path: "status.code", NotEqual: "SUCCESS"}}
	symmetric := steps("orders", productsAndStock("5", "10", "1")...)
	symmetric.Steps = append(symmetric.Steps, order("create_order", line("create_product", "2"), line("create_product_2", "3")))
	cases := []sliceCase{
		{name: "exports and expectation references", c: sliceFixture(), target: "fetch_deal", kept: "create_book,create_deal,fetch_deal", check: func(t *testing.T, res *chain.SliceResult) {
			if _, ok := res.Chain.Vars["unused"]; ok || res.Chain.Vars["tag"] == nil {
				t.Errorf("only the vars a kept step reads survive: %v", res.Chain.Vars)
			}
			if res.Kept[2].Reason != "target" || !strings.HasPrefix(res.Kept[1].Reason, "produces ${create_deal.results.0.id_deal} used by fetch_deal") {
				t.Errorf("reasons: %+v", res.Kept)
			}
			for _, want := range []string{"fixture", "fetch_deal", "3 of 5 steps"} {
				if !strings.Contains(res.Chain.Description, want) {
					t.Errorf("description must record %q:\n%s", want, res.Chain.Description)
				}
			}
			if res.Chain.Name != "fixture-slice-fetch_deal" {
				t.Errorf("default slice name is %q", res.Chain.Name)
			}
		}},
		{name: "contract prerequisites", c: sliceFixture(), target: "fetch_deal", opts: chain.SliceOptions{Prereqs: edges("DealService/CreateDeal",
			chain.Prereq{RPC: "LimitActionService/SetCounterpartyCreditLimit", Edge: "before"}, chain.Prereq{RPC: "AssetService/CreateAsset", Edge: "needs"})},
			kept: "create_book,set_limit,create_deal,fetch_deal", unmet: "AssetService/CreateAsset", check: func(t *testing.T, res *chain.SliceResult) {
				if k := keptByID(res)["set_limit"]; !strings.HasPrefix(k.Reason, "contract needs LimitActionService/SetCounterpartyCreditLimit") {
					t.Errorf("set_limit reason is %q", k.Reason)
				}
			}},
		{name: "an unknown step", c: sliceFixture(), target: "no_such_step", err: []string{"create_book", "fetch_deal"}},
		{name: "one step per aliased prerequisite", c: orderFlow(), target: "confirm_order", opts: chain.SliceOptions{Prereqs: aliasPrereqs},
			kept: "add_stock_a,add_stock_b,create_order,confirm_order", check: func(t *testing.T, res *chain.SliceResult) {
				if k := keptByID(res)["add_stock_a"]; k.Reason != "contract needs StockService/AddStock@a (needs)" {
					t.Errorf("add_stock_a reason is %q", k.Reason)
				}
				if last := res.Kept[len(res.Kept)-1]; last.Index != 4 {
					t.Errorf("indexes count from one: %+v", last)
				}
			}},
		{name: "writes after the target", c: orderFlow(), target: "create_order", check: func(t *testing.T, res *chain.SliceResult) {
			if len(res.DroppedWrites) != 2 || res.Reach != 3 || res.DroppedWrites[0].ID == "cancel_order" || res.DroppedWrites[1].ID == "cancel_order" {
				t.Errorf("only the two earlier writes are dropped: %+v reach %d", res.DroppedWrites, res.Reach)
			}
		}},
		{name: "one plain step for two aliases", c: steps("one-stock", st("stock_first", "StockService/AddStock", nil), st("create_order", "OrderService/CreateOrder", nil),
			st("confirm_order", "OrderService/ConfirmOrder", map[string]any{"id_order": "${create_order.order.id_order}"})), target: "confirm_order",
			opts: chain.SliceOptions{Prereqs: aliasPrereqs}, kept: "stock_first,create_order,confirm_order", unmet: "StockService/AddStock@b"},
		{name: "a from edge the body already makes", c: steps("catalog-refusals", st("create_product", "ProductService/CreateProduct", nil),
			st("create_product_blank_name", "ProductService/CreateProduct", nil), st("add_stock", "StockService/AddStock", map[string]any{"id_product": "${create_product.product.id_product}"})),
			target: "add_stock", opts: chain.SliceOptions{Prereqs: edges("StockService/AddStock", chain.Prereq{RPC: "ProductService/CreateProduct", Edge: "from"})},
			kept: "create_product,add_stock", check: func(t *testing.T, res *chain.SliceResult) {
				if k := keptByID(res)["create_product"]; k.Kind != chain.KeepProduces {
					t.Errorf("a body reference is labelled produces, got %s", k.Kind)
				}
			}},
		{name: "a closure drops writes", c: keepFixture(), target: "boom", check: func(t *testing.T, res *chain.SliceResult) {
			if !res.UnderIncluded || len(res.DroppedWrites) != 2 {
				t.Errorf("owner and noise are dropped: %+v", res.DroppedWrites)
			}
		}},
		{name: "-keep puts a write back with what it needs", c: keepFixture(), target: "boom", opts: chain.SliceOptions{Keep: []string{"noise"}}, check: func(t *testing.T, res *chain.SliceResult) {
			ids := []string{}
			for _, k := range res.Kept {
				ids = append(ids, k.ID+":"+k.Kind)
			}
			if got := strings.Join(ids, " "); got != "seed:produces owner:produces noise:requested boom:target" {
				t.Errorf("got %s", got)
			}
			if res.UnderIncluded || !strings.Contains(res.Chain.Description, "Kept on request: noise.") {
				t.Errorf("nothing under-included and the request recorded:\n%s", res.Chain.Description)
			}
		}},
		{name: "-keep of an unknown step", c: keepFixture(), target: "boom", opts: chain.SliceOptions{Keep: []string{"nope"}}, err: []string{"nope"}},
		{name: "-keep of a later step", c: keepFixture(), target: "boom", opts: chain.SliceOptions{Keep: []string{"after"}}, err: []string{"after the target"}},
		{name: "-keep writes", c: keepFixture(), target: "boom", opts: chain.SliceOptions{Keep: []string{chain.SliceKeepWrites}}, kept: "seed,owner,noise,boom",
			check: func(t *testing.T, res *chain.SliceResult) {
				if res.UnderIncluded || len(res.DroppedWrites) != 0 {
					t.Errorf("nothing is dropped: %+v", res.DroppedWrites)
				}
			}},
		{name: "-keep writes with an id", c: steps("mixed", st("make", "pkg.Svc/Create", nil), st("peek", "pkg.Svc/Fetch", nil), st("boom", "pkg.Svc/Approve", nil)),
			target: "boom", opts: chain.SliceOptions{Keep: []string{"peek", chain.SliceKeepWrites}}, kept: "make,peek,boom"},
		{name: "-keep writes keeps a refused write", c: steps("refusals", st("make", "pkg.Svc/Create", nil), st("owner", "pkg.Svc/Create", nil), st("boom", "pkg.Svc/Approve", nil)),
			target: "boom", opts: chain.SliceOptions{Keep: []string{chain.SliceKeepWrites}, Refused: refusedIn(map[string]string{"owner": "refused in-band"})},
			kept: "make,owner,boom", check: func(t *testing.T, res *chain.SliceResult) {
				if res.UnderIncluded || len(res.RefusedWrites) != 0 {
					t.Errorf("nothing is dropped: %+v", res.RefusedWrites)
				}
			}},
		{name: "kept_red pins of the kept steps", c: keptRedChain(), target: "confirm", check: func(t *testing.T, res *chain.SliceResult) {
			if len(res.Chain.KeptRed) != 2 || res.Chain.KeptRed[1].Step != "confirm" || len(res.DroppedPins) != 1 || res.DroppedPins[0].Step != "later" {
				t.Errorf("confirm's pins carried, later's reported: %+v %+v", res.Chain.KeptRed, res.DroppedPins)
			}
			raw, err := res.Chain.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "kr-slice.yaml")
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := chain.LoadFile(path); err != nil {
				t.Errorf("the slice with its pins must load: %v\n%s", err, raw)
			}
		}},
		{name: "a pinned step after the target", c: pinnedLater(), target: "confirm", opts: chain.SliceOptions{Keep: []string{"fetch_after_confirm"}, Pinned: []string{"fetch_after_confirm"}},
			check: func(t *testing.T, res *chain.SliceResult) {
				ids := []string{}
				for _, s := range res.Chain.Steps {
					ids = append(ids, s.ID)
				}
				if strings.Join(ids, ",") != "create,confirm,fetch_after_confirm" {
					t.Errorf("got %v", ids)
				}
			}},
		{name: "a plain -keep after the target", c: pinnedLater(), target: "confirm", opts: chain.SliceOptions{Keep: []string{"fetch_after_confirm"}}, err: []string{""}},
		{name: "a create a list filters by through a var", c: listFilter, target: "list_products", lacks: []string{"create_customer"}, check: func(t *testing.T, res *chain.SliceResult) {
			if k, ok := keptByID(res)["create_product_prefix_case"]; !ok || !strings.Contains(k.Reason, "sku_prefix") {
				t.Errorf("the create shaping the list is kept: %+v", res.Kept)
			}
		}},
		{name: "an auth probe keeps no fixture", c: listFilter, target: "list_products_without_token", kept: "list_products_without_token"},
		{name: "a literal field", c: literalProducerFixture(), target: "list_literal", opts: chain.SliceOptions{Prereqs: literalPrereqs}, kept: "list_literal", unmet: "-"},
		{name: "a var field", c: literalProducerFixture(), target: "list_ref", opts: chain.SliceOptions{Prereqs: literalPrereqs}, kept: "list_ref", unmet: "-"},
		{name: "a refused step is no producer", c: literalProducerFixture(), target: "list_unset", opts: chain.SliceOptions{Prereqs: literalPrereqs}, kept: "good_customer,list_unset"},
		{name: "only a refused producer", c: steps(full.Name, full.Steps[1], full.Steps[4]), target: "list_unset", opts: chain.SliceOptions{Prereqs: literalPrereqs},
			kept: "list_unset", unmet: "CustomerService/CreateCustomer"},
		{name: "a producer refused in the run", c: withRun, target: "list_unset", opts: chain.SliceOptions{Prereqs: literalPrereqs,
			Refused: refusedIn(map[string]string{"bad_customer": "refused: transport invalid_argument"})}, kept: "good_customer,list_unset"},
		{name: "the unique value the target sends again", c: sameEmailChain(), target: "create_customer_same_email_case",
			opts:  chain.SliceOptions{KeyField: func(rpc, field string) (bool, bool) { return field == "email", true }},
			lacks: []string{"create_customer_other", "create_customer_fixed", "create_customer_same_email"}, check: func(t *testing.T, res *chain.SliceResult) {
				if k, ok := keptByID(res)["create_customer"]; !ok || !strings.Contains(k.Reason, "email") {
					t.Errorf("the create whose email repeats is kept: %+v", res.Kept)
				}
			}},
		{name: "no key field declared", c: sameEmailChain(), target: "create_customer_same_email_case",
			opts: chain.SliceOptions{KeyField: func(string, string) (bool, bool) { return false, true }}, lacks: []string{"create_customer"}},
		{name: "no contracts", c: sameEmailChain(), target: "create_customer_same_email_case", has: []string{"create_customer", "create_customer_other"}},
		{name: "the needed call for every entity the order reaches", c: confirmShortageChain(), target: "confirm_order_last_item",
			opts: chain.SliceOptions{Prereqs: edges("OrderService/ConfirmOrder", chain.Prereq{RPC: "StockService/AddStock", Edge: "needs"})},
			has:  []string{"add_stock", "add_stock_2", "confirm_order", "create_order"}, lacks: []string{"add_stock_3", "confirm_order_3", "create_order_3", "create_order_other"},
			check: func(t *testing.T, res *chain.SliceResult) {
				if k := keptByID(res)["confirm_order"]; k.Reason == "" || k.Kind != chain.KeepSideEffect {
					t.Errorf("the reason names the side effect: %+v", k)
				}
			}},
		{name: "a write expected refused keeps its side effects", c: withConfirmRefused, target: "confirm_order_last_item", envelope: true,
			opts: chain.SliceOptions{Prereqs: edges("OrderService/ConfirmOrder", chain.Prereq{RPC: "StockService/AddStock", Edge: "needs"})},
			has:  []string{"confirm_order", "create_order"}, lacks: []string{"confirm_order_3", "create_order_3"}},
		{name: "the needed call for every entity the target reads", c: symmetric, target: "create_order",
			opts: chain.SliceOptions{Prereqs: edges("OrderService/CreateOrder", chain.Prereq{RPC: "StockService/AddStock", Edge: "needs"})},
			has:  []string{"add_stock", "add_stock_2"}, lacks: []string{"add_stock_3", "create_product_3"}},
		{name: "a write changing the state a kept read reads", c: cancelRestockChain(), target: "get_product_2_after_cancel",
			has:   []string{"create_order", "confirm_order", "cancel_order", "create_customer", "add_stock_2", "create_product_2"},
			lacks: []string{"create_other_customer", "add_stock"}, check: func(t *testing.T, res *chain.SliceResult) {
				if k := keptByID(res)["cancel_order"]; k.Kind != chain.KeepSideEffect || !strings.Contains(k.Reason, "create_product_2") || !strings.Contains(k.Reason, "get_product_2_after_cancel") {
					t.Errorf("the reason names the entity and its reader: %+v", k)
				}
			}},
		{name: "a kept write asserting a level keeps the earlier writes setting it", c: batchLevelChain(), target: "get_b",
			opts: chain.SliceOptions{AssertsWrite: func(w, r string) bool { return true }},
			kept: "create_a,create_b,batch_1,batch_3,batch_12,get_b", check: func(t *testing.T, res *chain.SliceResult) {
				if k := keptByID(res)["batch_1"]; k.Kind != chain.KeepSideEffect || !strings.Contains(k.Reason, "batch_12 reads") {
					t.Errorf("the reason names the asserting write: %+v", k)
				}
			}},
		{name: "a kept write asserting no field an earlier write sets keeps none", c: batchLevelChain(), target: "get_b",
			opts: chain.SliceOptions{AssertsWrite: func(w, r string) bool { return false }}, kept: "create_a,create_b,batch_12,get_b"},
		{name: "a write the prerequisite names as via", c: steps("via",
			st("make", "Svc/Make", nil, chain.Expectation{Path: "id", NotEmpty: true}),
			st("batch", "Svc/StockBatch", map[string]any{"lines": []any{map[string]any{"id": "${make.id}"}}}, chain.Expectation{Path: "ok", Equals: true}),
			st("confirm", "Svc/Confirm", ref("${make.id}"), chain.Expectation{Path: "ok", Equals: true})), target: "confirm",
			opts: chain.SliceOptions{Prereqs: edges("Svc/Confirm", chain.Prereq{RPC: "Svc/Stock", Edge: "needs", Via: []string{"Svc/StockBatch"}})}, kept: "make,batch,confirm", unmet: "-"},
		{name: "an alias match dominates a referenced step", c: productChainReading("tag"), target: "add_stock_second",
			opts: chain.SliceOptions{Prereqs: edges("StockService/AddStock", chain.Prereq{RPC: "ProductService/CreateProduct", Alias: "first", Edge: "from"})},
			check: func(t *testing.T, res *chain.SliceResult) {
				kept := keptByID(res)
				if kept["create_product_first"].Reason != "contract needs ProductService/CreateProduct@first (from)" || !strings.HasPrefix(kept["create_product_second"].Reason, "produces ") {
					t.Errorf("reasons: %+v", res.Kept)
				}
			}},
		{name: "a prerequisite scoped to another alias", c: productChainReading("tag"), target: "add_stock_second",
			opts: chain.SliceOptions{Prereqs: edges("StockService/AddStock", chain.Prereq{RPC: "ProductService/CreateProduct", Alias: "first", Edge: "from", For: "first"},
				chain.Prereq{RPC: "ProductService/CreateProduct", Alias: "second", Edge: "from", For: "second"})}, kept: "create_product_second,add_stock_second"},
		{name: "closure never reuses the run's var", c: productChainReading("batch"), target: "create_product_second", opts: chain.SliceOptions{RunID: "r1", RunVars: map[string]any{"batch": "T1"}},
			check: func(t *testing.T, res *chain.SliceResult) {
				if strings.Join(res.MissingVars, ",") != "batch" || res.Chain.Vars["batch"] != nil {
					t.Errorf("vars %v missing %v", res.Chain.Vars, res.MissingVars)
				}
			}},
		{name: "a -var fills an undeclared var", c: productChainReading("batch"), target: "create_product_second", opts: chain.SliceOptions{Vars: map[string]any{"batch": "T2"}},
			check: func(t *testing.T, res *chain.SliceResult) {
				if res.Chain.Vars["batch"] != "T2" || len(res.MissingVars) != 0 || res.FilledVars[0].From != chain.VarFromFlag {
					t.Errorf("vars %v filled %+v", res.Chain.Vars, res.FilledVars)
				}
			}},
		{name: "an undeclared tag is left to the run", c: productChainReading("tag"), target: "create_product_second", opts: chain.SliceOptions{RunID: "r1", RunVars: map[string]any{"tag": "T1"}},
			check: func(t *testing.T, res *chain.SliceResult) {
				if res.Chain.Vars["tag"] != nil || len(res.MissingVars) != 0 || len(res.FreshVars) != 0 {
					t.Errorf("vars %v missing %v fresh %v", res.Chain.Vars, res.MissingVars, res.FreshVars)
				}
			}},
		{name: "a verified slice sends the source run's vars", c: func() *chain.Chain {
			c := steps("orders", st("create_product", "ProductService/CreateProduct", map[string]any{"sku": "sku-${vars.tag}", "price_minor": "${vars.price}"}),
				st("create_order", "OrderService/CreateOrder", map[string]any{"id_product": "${create_product.product.id_product}"}))
			c.Vars = map[string]any{"price": 1250, "tag": "orders"}
			return c
		}(), target: "create_order", opts: chain.SliceOptions{RunID: "r1", RunVars: map[string]any{"price": 399, "tag": "old"}, RunVarsAsDefaults: true},
			check: func(t *testing.T, res *chain.SliceResult) {
				if res.Chain.Vars["price"] != 399 || res.Chain.Vars["tag"] != "orders" {
					t.Errorf("price from the run, a fresh tag from the chain: %v", res.Chain.Vars)
				}
			}},
		{name: "a dropped write the source run refused", c: refusedDrop, target: "cancel_order", opts: chain.SliceOptions{RunID: "r1",
			Refused: refusedIn(map[string]string{"create_order_no_lines": "refused: transport invalid_argument", "confirm_twice": "refused: error.code = 1303"})},
			check: func(t *testing.T, res *chain.SliceResult) {
				if res.UnderIncluded || len(res.DroppedWrites) != 0 || len(res.RefusedWrites) != 2 || res.RefusedWrites[1].Reason != "refused: error.code = 1303" ||
					!strings.Contains(res.Chain.Description, "refused in run r1") {
					t.Errorf("refused writes are listed, not dropped: %+v %+v\n%s", res.DroppedWrites, res.RefusedWrites, res.Chain.Description)
				}
			}},
	}
	for _, tc := range cases {
		if err := tc.c.Normalize(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.envelope {
			chain.SetEnvelope("status.code", "SUCCESS")
		}
		res, err := chain.Slice(tc.c, tc.target, tc.opts)
		chain.SetEnvelope("", "")
		if tc.err != nil {
			for _, want := range tc.err {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("%s: want an error naming %q, got %v", tc.name, want, err)
				}
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		kept := keptByID(res)
		if tc.kept != "" && strings.Join(keptIDs(res), ",") != tc.kept {
			t.Errorf("%s: kept %v, want %s", tc.name, keptIDs(res), tc.kept)
		}
		for _, id := range tc.has {
			if _, ok := kept[id]; !ok {
				t.Errorf("%s: %s must be kept: %+v", tc.name, id, res.Kept)
			}
		}
		for _, id := range tc.lacks {
			if _, ok := kept[id]; ok {
				t.Errorf("%s: %s must not be kept: %+v", tc.name, id, res.Kept)
			}
		}
		unmet := []string{}
		for _, u := range res.Unmet {
			unmet = append(unmet, u.RPC)
		}
		if tc.unmet == "-" && len(unmet) != 0 || tc.unmet != "" && tc.unmet != "-" && strings.Join(unmet, ",") != tc.unmet {
			t.Errorf("%s: unmet %v, want %s", tc.name, unmet, tc.unmet)
		}
		if tc.check != nil {
			tc.check(t, res)
		}
	}
}

func TestSliceDescriptionRecordsTheRerun(t *testing.T) {
	res, err := chain.Slice(productChainReading("tag"), "add_stock_second", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Chain.Description, "HYPOTHESIS") {
		t.Fatal("an unverified slice is a hypothesis")
	}
	res.MarkReproduced("src-run", "slice-run", time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC))
	d := res.Chain.Description
	for _, want := range []string{"VERIFIED", "2026-09-24", "slice run slice-run", "source run src-run"} {
		if !strings.Contains(d, want) || strings.Contains(d, "HYPOTHESIS") || !strings.HasPrefix(d, chain.SliceDescriptionPrefix("stock", "add_stock_second")) {
			t.Errorf("want %q and the slice prefix, no hypothesis:\n%s", want, d)
		}
	}
	description := "Slice of src reproducing step t: 2 of 3 steps, mode closure.\n\nComputed by 'shrt chain slice'.\n" +
		"\nThis slice is a HYPOTHESIS until it is run. A dependency that is state rather than a\nreference leaves no trace in the YAML, so a slice can be too small and still go green.\n" +
		"\n2 dropped step(s) WRITE: a, b.\n\nRE-RUN by 'shrt chain slice -verify': reproduced on 2026-09-23: run r1.\n"
	got := chain.RecordRerun(description, "not reproduced on 2026-09-24: run r2")
	if strings.Contains(got, "HYPOTHESIS") || strings.Contains(got, "run r1") || strings.Count(got, "RE-RUN by") != 1 || !strings.Contains(got, "run r2") || !strings.Contains(got, "2 dropped step(s) WRITE") {
		t.Fatalf("one re-run line replaces the hypothesis and the earlier re-run:\n%s", got)
	}
	if again := chain.RecordRerun(got, "reproduced on 2026-09-25: run r3"); strings.Count(again, "RE-RUN by") != 1 || strings.Contains(again, "run r2") {
		t.Fatalf("a later re-run replaces the earlier one:\n%s", again)
	}
}

func TestCompareVerdicts(t *testing.T) {
	app := []chain.ExpectResult{{Path: "error.details.0.app_code", Rule: "equals", Passed: true}}
	source := chain.Verdict{Step: "a", Status: "passed", ErrorCode: "failed_precondition", Expect: app}
	total := func(want, got any) chain.Verdict {
		return chain.Verdict{Status: "failed", ErrorCode: "SUCCESS", Expect: []chain.ExpectResult{{Path: "order.total_minor", Rule: "equals", Want: want, Got: got}}}
	}
	for _, tc := range []struct {
		a, b chain.Verdict
		n    int
		has  []string
	}{
		{source, source, 0, nil},
		{source, chain.Verdict{Step: "a", Status: "passed", ErrorCode: "not_found", Expect: app}, 1, nil},
		{source, chain.Verdict{Step: "a", Status: "failed", ErrorCode: "failed_precondition", Expect: []chain.ExpectResult{{Path: "error.details.0.app_code", Rule: "equals"}}}, 2, []string{"app_code"}},
		{chain.Verdict{Expect: []chain.ExpectResult{{Path: "error.code", Rule: "equals", Passed: true}, {Path: "qty", Rule: "equals", Passed: true}}},
			chain.Verdict{Expect: []chain.ExpectResult{{Path: "error.code", Rule: "equals", Passed: true}, {Path: "qty", Rule: "equals"}}}, 1, []string{"expectation 2 (qty equals)"}},
		{total(3697, 1995), total(4548, 6250), 1, []string{"3697", "6250"}},
	} {
		diffs := chain.CompareVerdictsMasking(tc.a, tc.b, nil)
		if len(diffs) != tc.n {
			t.Errorf("want %d difference(s), got %v", tc.n, diffs)
		}
		for _, want := range tc.has {
			if !strings.Contains(fmt.Sprint(diffs), want) {
				t.Errorf("want %q in %v", want, diffs)
			}
		}
	}
}

func TestSliceVerdictReResolvesAClockRelativeBound(t *testing.T) {
	step := &chain.Step{ID: "create", Expect: []chain.Expectation{
		{Path: "status.code", Equals: "SUCCESS"},
		{Path: "product.created_at", Within: &chain.Within{Of: "${nowunix}", By: 300}},
		{Path: "product.created_at", Gte: "${nowunix-300}"},
	}}
	verdict := func(now float64, got any) chain.Verdict {
		return chain.ClockRelative(step, chain.Verdict{Step: "create", Status: "failed", ErrorCode: "SUCCESS", Expect: []chain.ExpectResult{
			{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
			{Path: "product.created_at", Rule: "within", Want: map[string]any{"of": fmt.Sprintf("%.0f", now), "by": 300}, Got: got},
			{Path: "product.created_at", Rule: "gte", Want: fmt.Sprintf("%.0f", now-300), Got: got},
		}})
	}
	alike := func(_ string, a, b any) bool { return chain.SameClockOffset(a, b) }
	if d := chain.CompareVerdicts(verdict(1790325508, nil), verdict(1790325514, nil)); len(d) != 0 {
		t.Errorf("a bound resolved at each run's own time is the same bound: %v", d)
	}
	if d := chain.CompareVerdictsMasking(verdict(1790325508, "1790329108"), verdict(1790325514, "1790329115"), alike); len(d) != 0 {
		t.Errorf("a stamp an hour ahead of each run's clock fails the same way: %v", d)
	}
	if d := chain.CompareVerdictsMasking(verdict(1790325508, "1790329108"), verdict(1790325514, "1790325000"), alike); len(d) == 0 {
		t.Error("an hour ahead in one run and minutes behind in the other is a different failure")
	}
}

func TestWithoutEditSource(t *testing.T) {
	for _, tc := range []struct {
		src, cut, want string
		ok             bool
	}{
		{"name: edit\nsteps:\n  - call: A/Create\n    id: a\n    expect:\n      - {path: status.code, equals: SUCCESS}\n  - call: A/Update\n    id: b\n    body: {id_a: \"${a.id}\"}\n    expect:\n      - {path: status.code, equals: SUCCESS}\n  - call: A/Get\n    id: c\nkept_red:\n  - {step: b, path: status.code}\n",
			"b", "name: edit\nsteps:\n  - call: A/Create\n    id: a\n    expect:\n      - {path: status.code, equals: SUCCESS}\n  - call: A/Get\n    id: c\n", true},
		{"name: alias\nsteps:\n  - id: a\n    call: A/Create\n    expect:\n      - &ok {path: status.code, equals: SUCCESS}\n  - id: b\n    call: A/Get\n    expect:\n      - *ok\n", "a", "", false},
	} {
		path := filepath.Join(t.TempDir(), "edit.yaml")
		if err := os.WriteFile(path, []byte(tc.src), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := chain.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		res, err := chain.Without(c, []string{tc.cut}, c.Name)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := res.EditSource([]byte(tc.src), path)
		if ok != tc.ok || (ok && string(got) != tc.want) {
			t.Errorf("cut %s: ok=%v\n%s", tc.cut, ok, got)
		}
	}
}
