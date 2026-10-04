package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

var orderLifecycle = []string{"CreateProduct", "CreateProduct", "AddStock", "AddStockBatch", "CreateCustomer", "CreateOrder",
	"GetProduct", "ConfirmOrder", "GetProduct", "GetProduct", "FetchOrder", "CancelOrder", "ListOrders", "FetchOrder", "CancelOrder"}

func chainNewShopDemo(t *testing.T, rpcs ...string) (*chain.Chain, string, []string) {
	t.Helper()
	cat, lib := shopDemo(t)
	refs := make([]string, 0, len(rpcs))
	ids := make([]string, 0, len(rpcs))
	seen := map[string]bool{}
	for _, rpc := range rpcs {
		m, err := cat.Lookup(rpc)
		if err != nil {
			t.Fatal(err)
		}
		base := contract.For(m).StepID()
		id := base
		for i := 2; seen[id]; i++ {
			id = base + "_" + string(rune('0'+i))
		}
		seen[id] = true
		refs = append(refs, m.FullName)
		ids = append(ids, id)
	}
	raw, notes, err := contract.ScaffoldChain("flow", "", refs, ids, lib, cat)
	if err != nil {
		t.Fatal(err)
	}
	var c chain.Chain
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	return &c, string(raw), notes
}

func TestChainNewNamesARepeatedRPCAfterWhatItObserves(t *testing.T) {
	c, raw, notes := chainNewShopDemo(t, orderLifecycle...)
	want := []string{"create_product", "create_product_2", "add_stock", "add_stock_batch", "create_customer", "create_order",
		"get_product_after_create_order", "confirm_order", "get_product_after_confirm_order", "get_product_2_after_confirm_order",
		"fetch_order_after_confirm_order", "cancel_order", "list_orders", "fetch_order_after_cancel_order", "cancel_order_again"}
	got := []string{}
	for _, st := range c.Steps {
		got = append(got, st.ID)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("ids:\n got %v\nwant %v\n%s", got, want, raw)
	}
	reads := map[string]string{
		"get_product_after_confirm_order":   "${create_product.product.id_product}",
		"get_product_2_after_confirm_order": "${create_product_2.product.id_product}",
	}
	for id, ref := range reads {
		st, _ := c.Step(id)
		if st.Body["id_product"] != ref {
			t.Fatalf("%s must read %s, got %v:\n%s", id, ref, st.Body["id_product"], raw)
		}
	}
	for _, st := range c.Steps {
		for _, ref := range st.References() {
			src, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(ref), "steps."), ".")
			if _, ok := c.Step(src); !ok && src != "vars" && src != "uuid" && src != "nowunix" {
				t.Fatalf("step %s references %s, which is no step:\n%s", st.ID, ref, raw)
			}
		}
	}
	joined := strings.Join(notes, "\n")
	if strings.Contains(joined, "again after") {
		t.Fatalf("the id says what a read observes, so no note repeats it:\n%s", joined)
	}
	for _, n := range notes {
		if strings.Contains(n, "get_product_2:") || strings.Contains(n, "get_product_3") || strings.Contains(n, "fetch_order_2") {
			t.Fatalf("notes must name the steps by the ids written:\n%s", joined)
		}
	}
}

func TestChainNewGivesASecondCreateTheValuesThePlanGivesIt(t *testing.T) {
	c, raw, _ := chainNewShopDemo(t, orderLifecycle...)
	p, text, _ := shopDemoPlan(t, "CreateOrder")
	for _, id := range []string{"create_product", "create_product_2"} {
		st, _ := c.Step(id)
		planned := planStep(t, p, id)
		for _, field := range []string{"sku", "name", "price_minor"} {
			if st.Body[field] != planned.Body[field] {
				t.Fatalf("%s.%s: chain new sends %v, contract plan %v\n%s\n%s", id, field, st.Body[field], planned.Body[field], raw, text)
			}
		}
	}
	first, _ := c.Step("create_product")
	second, _ := c.Step("create_product_2")
	if first.Body["price_minor"] == second.Body["price_minor"] {
		t.Fatalf("a total over two lines cannot tell two products of one price apart:\n%s", raw)
	}
}

func TestChainNewWritesWhatTheContractsLetThePlanAssert(t *testing.T) {
	c, raw, notes := chainNewShopDemo(t, orderLifecycle...)
	step := func(id string) *chain.Step {
		st, ok := c.Step(id)
		if !ok {
			t.Fatalf("no step %s:\n%s", id, raw)
		}
		return st
	}
	total := 2*250 + 3*1250
	wantExpect(t, step("create_product"), "product.price_minor", "${steps.create_product.request.price_minor}")
	wantExpect(t, step("add_stock"), "qty_on_hand", 5)
	wantExpect(t, step("add_stock_batch"), "results.0.qty_on_hand", 5+3)
	wantExpect(t, step("add_stock_batch"), "results.1.qty_on_hand", 4)
	wantExpect(t, step("add_stock_batch"), "results.1.id_product", "${create_product_2.product.id_product}")
	wantExists(t, step("add_stock_batch"), "results.2", false)
	wantExpect(t, step("create_order"), "order.id_customer", "${create_customer.customer.id_customer}")
	wantExpect(t, step("create_order"), "order.status", "ORDER_STATUS_PENDING")
	wantExpect(t, step("create_order"), "order.total_minor", total)
	wantExpect(t, step("get_product_after_create_order"), "product.qty_on_hand", 5+3)
	wantExpect(t, step("confirm_order"), "order.status", "ORDER_STATUS_CONFIRMED")
	wantExpect(t, step("get_product_after_confirm_order"), "product.qty_on_hand", 5+3-2)
	wantExpect(t, step("get_product_2_after_confirm_order"), "product.qty_on_hand", 4-3)
	wantExpect(t, step("fetch_order_after_confirm_order"), "order.total_minor", total)
	wantExpect(t, step("cancel_order"), "order.status", "ORDER_STATUS_CANCELLED")
	for _, st := range c.Steps {
		for _, ref := range st.References() {
			head, rest, _ := strings.Cut(ref, ".")
			if _, isStep := c.Step(head); !isStep && head != "vars" && ref != "uuid" && ref != "nowunix" &&
				!(head == "steps" && strings.Contains(rest, ".request.")) {
				t.Fatalf("step %s reads ${%s}: a response field is ${<step>.<path>} and a request field ${steps.<step>.request.<path>}, nothing else:\n%s", st.ID, ref, raw)
			}
		}
	}
	joined := strings.Join(notes, "\n")
	if n := strings.Count(joined, "only the verdict"); n > 1 {
		t.Fatalf("steps left asserting only the verdict are named in one line, got %d:\n%s", n, joined)
	}
	for _, st := range c.Steps {
		if contract.AssertsOnlyVerdict(st) && !strings.Contains(joined, st.ID) {
			t.Fatalf("step %s asserts only the verdict and no note names it:\n%s", st.ID, joined)
		}
	}
}

func TestChainNewWritesNoDescriptionThatOnlyRepeatsTheContractSummary(t *testing.T) {
	c, raw, _ := chainNewShopDemo(t, orderLifecycle...)
	_, lib := shopDemo(t)
	for _, st := range c.Steps {
		rc, ok := lib.Get(st.Call)
		if ok && st.Description != "" && st.Description == contract.FirstSentence(rc.Summary) {
			t.Fatalf("step %s repeats its rpc's summary, which shrt contract show prints:\n%s", st.ID, raw)
		}
	}
}

var testerLifecycle = []string{"CreateProduct", "CreateProduct", "AddStock", "AddStock", "CreateCustomer", "CreateOrder", "GetProduct",
	"ConfirmOrder", "GetProduct", "GetProduct", "FetchOrder", "ListOrders", "CancelOrder", "GetProduct", "GetProduct", "ListOrders",
	"FetchOrder", "CancelOrder", "GetProduct"}

func TestChainNewAssertsTheStateTheLevelAndTheListEachWriteLeaves(t *testing.T) {
	c, raw, notes := chainNewShopDemo(t, testerLifecycle...)
	step := func(id string) *chain.Step {
		st, ok := c.Step(id)
		if !ok {
			t.Fatalf("no step %s:\n%s", id, raw)
		}
		return st
	}
	added := map[string]int{}
	for _, id := range []string{"add_stock", "add_stock_2"} {
		for _, e := range step(id).Expect {
			if e.Path == "qty_on_hand" {
				added[id] = e.Equals.(int)
			}
		}
	}
	if added["add_stock"] == 0 || added["add_stock_2"] == 0 {
		t.Fatalf("add_stock levels unasserted:\n%s", raw)
	}
	wantExpect(t, step("fetch_order_after_confirm_order"), "order.status", "ORDER_STATUS_CONFIRMED")
	wantExpect(t, step("fetch_order_after_cancel_order"), "order.status", "ORDER_STATUS_CANCELLED")
	wantExpect(t, step("fetch_order_after_confirm_order"), "order.lines.1.qty", "${steps.create_order.request.lines.1.qty}")
	wantExpect(t, step("get_product_after_cancel_order"), "product.qty_on_hand", added["add_stock"])
	wantExpect(t, step("get_product_2_after_cancel_order"), "product.qty_on_hand", added["add_stock_2"])
	wantExpect(t, step("get_product_after_cancel_order_again"), "product.qty_on_hand", added["add_stock"])
	for id, state := range map[string]string{"list_orders_after_confirm_order": "ORDER_STATUS_CONFIRMED", "list_orders_after_cancel_order": "ORDER_STATUS_CANCELLED"} {
		wantExpect(t, step(id), "orders.0.id_order", "${create_order.order.id_order}")
		wantExpect(t, step(id), "orders.0.status", state)
		wantExists(t, step(id), "orders.1", false)
	}
	again := step("cancel_order_again")
	wantExpect(t, again, "status.details.0.app_code", 1304)
	refused := false
	for _, e := range again.Expect {
		refused = refused || (e.Path == "status.code" && e.NotEqual == "SUCCESS")
	}
	if !refused {
		t.Fatalf("cancel_order already left the order CANCELLED, and CancelOrder's contract refuses a CANCELLED order:\n%s", raw)
	}
	joined := strings.Join(notes, "\n")
	if strings.Contains(joined, "only the verdict") {
		t.Fatalf("every step of the lifecycle has something its contracts let it assert:\n%s\n%s", joined, raw)
	}
}
