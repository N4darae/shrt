package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func dimCalls(dims ...map[string]string) []rowCall {
	out := []rowCall{}
	for i, d := range dims {
		out = append(out, rowCall{at: fmt.Sprintf("c %d", i), dims: d})
	}
	return out
}

func TestATriggerLineContrastsTheFailingCallsWithThePassingOnes(t *testing.T) {
	clerk, admin := map[string]string{"as": "clerk"}, map[string]string{"as": "default"}
	lines := func(n int, repeat string) map[string]string {
		return map[string]string{"as": "default", "len lines": fmt.Sprint(n), "repeat lines": repeat}
	}
	for _, c := range []struct {
		name          string
		fails, passes []map[string]string
		want          string
	}{
		{"the auth profile", []map[string]string{clerk, clerk}, []map[string]string{admin, admin, admin},
			"trigger: fails as clerk (2 calls); passes as default (3 calls)"},
		{"a list's length", []map[string]string{lines(2, ""), lines(3, "")}, []map[string]string{lines(1, ""), lines(1, "")},
			"trigger: fails with lines of 2+ items (2 calls); passes with lines of 1 item (2 calls)"},
		{"a list that repeats an item key", []map[string]string{lines(2, "id_product"), lines(3, "id_product")}, []map[string]string{lines(3, ""), lines(12, "")},
			"trigger: fails when lines repeat id_product (2 calls; lines of 2 and 3 items); passes with distinct id_product (2 calls; lines of 3 and 12 items)"},
		{"a repeat whose lengths also differ says the lengths, not a length trigger", []map[string]string{lines(2, "id_product")}, []map[string]string{lines(3, ""), lines(12, "")},
			"trigger: fails when lines repeat id_product (1 call; lines of 2 items); passes with distinct id_product (2 calls; lines of 3 and 12 items)"},
		{"a repeat against one-item lists only is a length", []map[string]string{lines(2, "id_product")}, []map[string]string{lines(1, ""), lines(1, "")},
			"trigger: fails with lines of 2+ items (1 call); passes with lines of 1 item (2 calls)"},
		{"a field present or absent", []map[string]string{admin}, []map[string]string{{"as": "default", "set sku_prefix": "set"}, {"as": "clerk", "set sku_prefix": "set"}},
			"trigger: fails with sku_prefix empty or absent (1 call); passes with sku_prefix set (2 calls)"},
		{"two dimensions that both separate are both said", []map[string]string{{"as": "clerk", "len lines": "2"}}, []map[string]string{{"as": "default", "len lines": "1"}},
			"trigger: fails as clerk with lines of 2+ items (1 call); passes as default with lines of 1 item (1 call)"},
		{"a passing call alike in every dimension", []map[string]string{lines(2, ""), lines(3, "")}, []map[string]string{lines(1, ""), lines(3, "")}, ""},
		{"values on both sides", []map[string]string{clerk, {"as": "default", "set note": "set"}}, []map[string]string{admin}, ""},
		{"lengths that interleave", []map[string]string{lines(1, ""), lines(3, "")}, []map[string]string{lines(2, "")}, ""},
		{"no passing call", []map[string]string{clerk}, nil, ""},
	} {
		if got, _ := triggerOf(dimCalls(c.fails...), dimCalls(c.passes...)); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestARequestsDimensionsAreItsProfileListsRepeatsAndSetFields(t *testing.T) {
	st := &runner.StepRecord{AuthProfile: "clerk", Request: json.RawMessage(
		`{"id_customer":"c1","note":"","sku_prefix":"ab","order":{"coupon":"X"},"lines":[{"id_product":"p1","qty":"1"},{"id_product":"p1","qty":"2"}],"tags":["a","a"]}`)}
	got := requestDims(st)
	want := map[string]string{"as": "clerk", "set sku_prefix": "set", "set order.coupon": "set", "len lines": "2", "repeat lines": "id_product", "len tags": "2", "repeat tags": ""}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("an id and an empty field are no dimension, a nested field is:\n got %v\nwant %v", got, want)
	}
}

func requested(body string) func(*runner.StepRecord) {
	return func(st *runner.StepRecord) { st.Request = json.RawMessage(body) }
}

func (s recStep) holds(path string, v any) recStep {
	s.Expect = append(s.Expect, chain.ExpectResult{Path: path, Rule: "equals", Want: v, Got: v, Passed: true})
	return s
}

func gateRun(name string, items []gateItem, steps ...recStep) *gateChain {
	for i := range items {
		items[i].from = "run"
	}
	return &gateChain{name: name, failed: len(items) > 0, items: items, runs: []*runner.Record{shopRecord(steps...)}}
}

func batchStep(id string, n int, ps ...string) recStep {
	var lines []string
	for _, p := range ps {
		lines = append(lines, `{"id_product":"`+p+`","qty":"2"}`)
	}
	for i := len(ps); i < n; i++ {
		lines = append(lines, fmt.Sprintf(`{"id_product":"q%d","qty":"2"}`, i))
	}
	return shopStep(id, shopBatch, `{"results":[],`+shopOK+`}`, "create_p1").with(requested(`{"lines":[` + strings.Join(lines, ",") + `]}`))
}

func TestATriggerCountsOnlyTheCallsTheGateCheckedOnTheRowsField(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	refusedBody := `{"status":{"code":"REJECTED","details":[{"app_code":1302,"reason":"OrderNotFound"}]}}`
	order := `{"order":{"id_order":"o1"},` + shopOK + `}`
	stock := `{"product":{"id_product":"p1","qty_on_hand":"4"},` + shopOK + `}`
	for _, c := range []struct {
		name   string
		chains []*gateChain
		want   string
	}{
		{"a refusal only as clerk, against the calls that succeeded, leaving out an expected refusal and an auth probe",
			[]*gateChain{
				gateRun("fetch-clerk", []gateItem{{Step: "fetch_c", Call: shopFetch, Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "REJECTED", Failed: true,
					Reason: reason{Kind: reasonRefused, Step: "fetch_c", RPC: shopFetch, Got: "1302 OrderNotFound"}}},
					shopStep("fetch_c", shopFetch, refusedBody).as("clerk").failing("status.code", "SUCCESS", "REJECTED").with(requested(`{"id_order":"o1"}`)),
					shopStep("fetch_probe", shopFetch, `{}`).as(runner.NoAuthProfile).with(requested(`{"id_order":"o1"}`))),
				gateRun("fetch-admin", nil,
					shopStep("fetch_a", shopFetch, order).with(requested(`{"id_order":"o1"}`)),
					shopStep("fetch_gone", shopFetch, refusedBody).as("clerk").with(requested(`{"id_order":"o9"}`))),
			},
			"    trigger: fails as clerk (1 call); passes as default (1 call)\n"},
		{"a call that should be refused and was accepted has no trigger",
			[]*gateChain{
				gateRun("customers", []gateItem{{Step: "get_unknown", Call: shopFetch, Path: "status.code", Rule: "not_equal", Want: "SUCCESS", Got: "SUCCESS", Failed: true,
					Reason: reason{Kind: reasonCode, Step: "get_unknown", RPC: shopFetch}}},
					shopStep("get_unknown", shopFetch, order).as("clerk").failing("status.code", "SUCCESS", "SUCCESS").with(requested(`{"id_order":"o9"}`)),
					shopStep("get_known", shopFetch, order).with(requested(`{"id_order":"o1"}`))),
			},
			""},
		{"a write the row blames on a later read passes only where a later read checked its field",
			[]*gateChain{
				gateRun("batches", []gateItem{{Step: "get_twice", Call: shopGet, Path: "product.qty_on_hand", Rule: "equals", Want: "4", Got: "2", Failed: true,
					Reason: reason{Kind: reasonWrite, Step: "batch_twice", RPC: shopBatch}}},
					shopStep("create_p1", shopCreate, `{"product":{"id_product":"p1"},`+shopOK+`}`),
					batchStep("batch_twice", 2, "p1", "p1"),
					shopStep("get_twice", shopGet, stock, "create_p1").failing("product.qty_on_hand", "4", "2"),
					batchStep("batch_two", 2, "p1"),
					shopStep("get_two", shopGet, stock, "create_p1").holds("product.qty_on_hand", "8"),
					batchStep("batch_unread", 2, "p1", "p1").holds("results.0.qty_on_hand", "10")),
			},
			"    trigger: fails when lines repeat id_product (1 call; lines of 2 items); passes with distinct id_product (1 call; lines of 2 items)\n"},
	} {
		settleGate(c.chains)
		out := captureStdout(t, func() { printGateGroups(nil, c.chains, false) })
		_, got, _ := strings.Cut(out, "\n")
		if _, got, _ = strings.Cut(got, "\n"); got != c.want {
			t.Errorf("%s: want the row followed by %q, got:\n%s", c.name, c.want, out)
		}
	}
}

func TestTheRowExampleShowsTheTriggerInAChainWithASafeSpot(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	stock := `{"product":{"id_product":"p1","qty_on_hand":"4"},` + shopOK + `}`
	read := func(id, write string) gateItem {
		return gateItem{Step: id, Call: shopGet, Path: "product.qty_on_hand", Rule: "equals", Want: "6", Got: "4", Failed: true,
			Reason: reason{Kind: reasonUnclear, Step: write, RPC: shopBatch, Read: id, ReadRPC: shopGet, Path: "product.qty_on_hand", Want: "6", Got: "4"}}
	}
	slice := gateRun("guard-slice", []gateItem{{Step: "get_b", Call: shopGet, Path: "product.qty_on_hand", Rule: "equals", Want: "1", Got: "-1", Failed: true,
		Reason: reason{Kind: reasonWrite, Step: "stock_batch", RPC: shopBatch}}},
		shopStep("create_p1", shopCreate, `{"product":{"id_product":"p1"},`+shopOK+`}`),
		batchStep("stock_batch", 2, "p1", "p1"),
		shopStep("get_b", shopGet, stock, "create_p1").failing("product.qty_on_hand", "1", "-1"))
	slice.keptRed = runner.KeptRedNotAsPinned
	spot := gateRun("batches", []gateItem{read("get_partial", "batch_partial"), read("get_twice", "batch_twice")},
		shopStep("create_p1", shopCreate, `{"product":{"id_product":"p1"},`+shopOK+`}`),
		batchStep("batch_partial", 3, "p1", "p1"),
		shopStep("get_partial", shopGet, stock, "create_p1").failing("product.qty_on_hand", "6", "4"),
		batchStep("batch_twice", 2, "p1", "p1"),
		shopStep("get_twice", shopGet, stock, "create_p1").failing("product.qty_on_hand", "6", "4"),
		batchStep("batch_two", 2, "p1"),
		shopStep("get_two", shopGet, stock, "create_p1").holds("product.qty_on_hand", "8"),
		batchStep("batch_twelve", 12, "p1"),
		shopStep("get_twelve", shopGet, stock, "create_p1").holds("product.qty_on_hand", "32"))
	spot.spot = true
	chains := []*gateChain{slice, spot}
	settleGate(chains)
	var groups []*gateGroup
	out := captureStdout(t, func() { groups = printGateGroups(nil, chains, false) })
	for _, want := range []string{
		"e.g. batches get_twice;",
		"\n    trigger: fails when lines repeat id_product (3 calls; lines of 2 and 3 items); passes with distinct id_product (2 calls; lines of 2 and 12 items)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the example is the repeat that differs from a passing batch only in the repeat, in the chain with a safe spot, not the kept-red slice: want %q in\n%s", want, out)
		}
	}
	if len(groups) != 1 || groups[0].in != "batches" || groups[0].example.Step != "get_twice" {
		t.Errorf("-repro slices the step the row names as its example")
	}
}

func TestWithNoTriggerTheExampleIsTheCommonestFailingRequest(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	order := func(id string, ps ...string) recStep {
		var lines []string
		for _, p := range ps {
			lines = append(lines, `{"id_product":"`+p+`","qty":"2"}`)
		}
		return shopStep(id, shopOrder, `{"order":{"id_order":"o1","total_minor":"5"},`+shopOK+`}`).with(requested(`{"lines":[` + strings.Join(lines, ",") + `]}`))
	}
	own := func(id string) gateItem {
		return gateItem{Step: id, Call: shopOrder, Path: "order.total_minor", Rule: "equals", Want: "6", Got: "5", Failed: true, Reason: reason{Kind: reasonWrite, Step: id, RPC: shopOrder}}
	}
	g := gateRun("orders", []gateItem{own("order_twice"), own("order_a"), own("order_b")},
		order("order_twice", "p1", "p1").failing("order.total_minor", "6", "5"),
		order("order_a", "p1", "p2").failing("order.total_minor", "6", "5"),
		order("order_b", "p1", "p3").failing("order.total_minor", "6", "5"),
		order("order_one", "p1").holds("order.total_minor", "2"),
		order("order_ends_in_one", "p1", "p2").holds("order.total_minor", "4"))
	g.spot = true
	settleGate([]*gateChain{g})
	out := captureStdout(t, func() { printGateGroups(nil, []*gateChain{g}, false) })
	if !strings.Contains(out, "e.g. orders order_a\n") || strings.Contains(out, "trigger:") {
		t.Errorf("a two-line order passes too, so nothing separates the calls and the example is a plain two-line order, not the one that repeats a product:\n%s", out)
	}
}

const orderLinesChain = `apiVersion: shrt/v1
name: %s
steps:
    - id: create_customer
      call: CustomerService/CreateCustomer
      body:
        email: %s-${vars.tag}@example.test
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: %s-${vars.tag}
        price_minor: "250"
      expect:
        - path: status.code
          equals: SUCCESS
%s`

const orderStep = `    - id: %s
      call: OrderService/CreateOrder
      body:
        id_customer: ${create_customer.customer.id_customer}
        idempotency_key: %s-${vars.tag}
        lines:
%s      expect:
        - path: order.total_minor
          equals: "%d"
`

func orderChain(name string, orders ...[]int) string {
	steps := ""
	for _, qtys := range orders {
		lines, total, id := "", 0, "order"
		for _, q := range qtys {
			lines += fmt.Sprintf("            - id_product: ${create_product.product.id_product}\n              qty: \"%d\"\n", q)
			total += 250 * q
			id += fmt.Sprintf("_%d", q)
		}
		steps += fmt.Sprintf(orderStep, id, id, lines, total)
	}
	return fmt.Sprintf(orderLinesChain, name, name, name, steps)
}

func TestGoldenGateTrigger(t *testing.T) {
	shop := newFakeShop()
	shop.lastLineBug = true
	chdirToFakeShop(t, shop)
	inProcessGate(t)
	writeFile(t, ".shrt/chains/orders.yaml", orderChain("orders", []int{3}, []int{1, 2}))
	var golden strings.Builder
	for _, c := range []struct {
		name, chain string
		trigger     bool
	}{
		{"CreateOrder prices the last line of a two-line order as qty 1: a one-line order passes, so the row says the trigger", "", true},
		{"a two-line order ending in qty 1 passes as well, so nothing separates the calls and the row says no trigger", orderChain("ends-in-one", []int{2, 1}), false},
	} {
		if c.chain != "" {
			writeFile(t, ".shrt/chains/ends-in-one.yaml", c.chain)
		}
		out, code := shrtOut(t, "gate", "-repro", "-no-session-check", "-hollow-baseline", "")
		fmt.Fprintf(&golden, "# %s\n$ shrt gate -repro  [exit %d]\n%s\n", c.name, code, out)
		trigger := "\n    trigger: fails with lines of 2+ items (1 call); passes with lines of 1 item (1 call)\n"
		if code != 1 || strings.Contains(out, trigger) != c.trigger || strings.Contains(out, "trigger:") != c.trigger ||
			!strings.Contains(out, "e.g. orders order_1_2") || !strings.Contains(out, "repro: shrt run .shrt/scratch/orders-slice-order_1_2.yaml  (reproduced 3/3)") {
			t.Errorf("%s: got %d:\n%s", c.name, code, out)
		}
	}
	checkGolden(t, "gate-trigger.txt", golden.String())
}

func TestGateReproSettlesEachUnclearWriteAndReadPairInARow(t *testing.T) {
	shop := newFakeShop()
	shop.stockInProduct, shop.addStockLostBug = true, true
	chdirToFakeShop(t, shop)
	inProcessGate(t)
	list := `    - id: list_products
      call: ProductService/ListProducts
      body:
        sku_prefix: ${steps.create_product.request.sku}
      expect:
        - path: products.0.qty_on_hand
          equals: "5"
`
	listed := strings.Replace(shelfChain, "name: shelf", "name: shelf-list", 1)
	writeFile(t, ".shrt/chains/shelf.yaml", shelfChain)
	writeFile(t, ".shrt/chains/shelf-list.yaml", listed[:strings.Index(listed, "    - id: get_product")]+list)
	writeFile(t, ".shrt/chains/shelf-both.yaml", strings.Replace(shelfChain, "name: shelf", "name: shelf-both", 1)+list)
	out, code := shrtOut(t, "gate", "-repro", "-no-session-check", "-hollow-baseline", "")
	_, block, _ := strings.Cut(out, "repro, for each row of failures by suspect rpc:\n")
	if code != 1 || strings.Count(block, "settled on the write add_stock") != 2 || !strings.Contains(block, "ProductService/ListProducts read product.qty_on_hand=0") ||
		!strings.Contains(block, "ProductService/GetProduct read products[].qty_on_hand=0") {
		t.Errorf("one row holds the write against GetProduct and against ListProducts, and -repro settles both pairs, got %d:\n%s", code, out)
	}
}
