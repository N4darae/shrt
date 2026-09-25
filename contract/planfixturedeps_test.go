package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func confirmNeedsStockPlanWith(t *testing.T, opts contract.PlanOptions, targets ...string) (*contract.Plan, string) {
	t.Helper()
	cat, _ := shopDemo(t)
	lib := shopDemoEdited(t, func(name, body string) string {
		if name != "orders.yaml" {
			return body
		}
		body = strings.Replace(body, "        needs: [shop.catalog.v1.StockService/AddStock]\n", "", 1)
		return strings.Replace(body, "    shop.orders.v1.OrderService/ConfirmOrder:\n", "    shop.orders.v1.OrderService/ConfirmOrder:\n        needs: [shop.catalog.v1.StockService/AddStock]\n", 1)
	})
	p, err := contract.BuildPlanWith(targets, lib, cat, "shopdemo", opts)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.YAML()
	if err != nil {
		t.Fatal(err)
	}
	return p, string(raw) + "\n" + strings.Join(p.Notes, "\n")
}

func TestAPrerequisiteWriteAddedToTheMainPathIsCopiedIntoTheRoleParityFixtures(t *testing.T) {
	p, text := confirmNeedsStockPlanWith(t, contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}}, "CreateOrder", "AddStock")
	main := planStep(t, p, "add_stock_for_create_product_2")
	if stepIndex(p.Chain, main.ID) > stepIndex(p.Chain, "create_order") {
		t.Fatalf("the prerequisite lands before the write it prepares:\n%s", text)
	}
	copied := planStep(t, p, "add_stock_for_create_product_2_for_clerk")
	if got := bodyAt(t, copied, "id_product"); got != "${create_product_2_for_clerk.product.id_product}" {
		t.Fatalf("the clerk copy stocks the clerk's product, got %s:\n%s", got, text)
	}
	if bodyAt(t, copied, "qty") != bodyAt(t, main, "qty") {
		t.Fatalf("the clerk copy adds what the main path adds:\n%s", text)
	}
	if stepIndex(p.Chain, copied.ID) > stepIndex(p.Chain, "create_order_as_clerk") || stepIndex(p.Chain, copied.ID) < stepIndex(p.Chain, "create_product_2_for_clerk") {
		t.Fatalf("the clerk copy runs between the clerk's product and the clerk's write: %s", strings.Join(stepIDs(p), ", "))
	}
}

func TestAStateMoveWhoseRPCNeedsAnotherWriteIsPlannedWithThatWriteAsAFixture(t *testing.T) {
	for _, target := range []string{"CancelOrder", "CreateOrder", "ListOrders"} {
		p, text := confirmNeedsStockPlanWith(t, contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}}, target)
		confirms, stocks := []string{}, []string{}
		for _, st := range p.Chain.Steps {
			switch {
			case strings.HasSuffix(st.Call, "/ConfirmOrder"):
				confirms = append(confirms, st.ID)
			case strings.HasSuffix(st.Call, "/AddStock"):
				stocks = append(stocks, st.ID)
				if !strings.HasPrefix(st.ID, "add_stock_for_") {
					t.Fatalf("%s: AddStock is a fixture dependency, not a target with probes of its own, got %s:\n%s", target, st.ID, text)
				}
			}
		}
		if len(confirms) == 0 || len(stocks) == 0 {
			t.Fatalf("%s: the confirm-state probes are planned with AddStock as a fixture (confirm %v, stock %v):\n%s", target, confirms, stocks, text)
		}
		if strings.Contains(text, "which this plan does not call") {
			t.Fatalf("%s: no state is dropped for a need the plan can satisfy:\n%s", target, text)
		}
		if len(p.Targets) != 1 {
			t.Fatalf("%s: AddStock is not made a target: %v", target, p.Targets)
		}
	}
}

func TestTheReadBackOfAnOrderAssertsEachLineAsSentAndTheTotal(t *testing.T) {
	p, text, _ := shopDemoPlanWith(t, contract.PlanOptions{}, "CreateOrder")
	read := planStep(t, p, "fetch_order_after_create_order")
	for _, i := range []string{"0", "1"} {
		wantExpect(t, read, "order.lines."+i+".id_product", "${steps.create_order.request.lines."+i+".id_product}")
		wantExpect(t, read, "order.lines."+i+".qty", "${steps.create_order.request.lines."+i+".qty}")
	}
	wantExists(t, read, "order.lines.2", false)
	found := false
	for _, e := range read.Expect {
		found = found || e.Path == "order.total_minor"
	}
	if !found {
		t.Fatalf("the read-back asserts the total:\n%s", text)
	}
}

func TestATotalAndABatchArePlannedWithOneResourceOnTwoLines(t *testing.T) {
	p, text, _ := shopDemoPlanWith(t, contract.PlanOptions{}, "CreateOrder")
	order := planStep(t, p, "create_order_same_product_twice")
	if bodyAt(t, order, "lines.0.id_product") != bodyAt(t, order, "lines.1.id_product") || bodyAt(t, order, "lines.0.qty") == bodyAt(t, order, "lines.1.qty") {
		t.Fatalf("both lines name one product with different quantities:\n%s", text)
	}
	wantExpect(t, order, "order.total_minor", int64(1250))

	p, text, _ = shopDemoPlanWith(t, contract.PlanOptions{}, "AddStockBatch")
	batch := planStep(t, p, "add_stock_batch_same_product_twice")
	if bodyAt(t, batch, "lines.0.id_product") != bodyAt(t, batch, "lines.1.id_product") {
		t.Fatalf("both lines name one product:\n%s", text)
	}
	wantExpect(t, batch, "results.0.qty_on_hand", int64(3))
	wantExpect(t, batch, "results.1.qty_on_hand", int64(7))
	wantExpect(t, planStep(t, p, "get_product_after_add_stock_batch_same_product_twice"), "product.qty_on_hand", int64(7))
}

func TestAnIncreaseAndABatchArePlannedWithLargeQuantitiesAndTheExactLevel(t *testing.T) {
	p, text, _ := shopDemoPlanWith(t, contract.PlanOptions{}, "AddStock")
	for _, q := range []string{"1250", "12345"} {
		st := planStep(t, p, "add_stock_qty_large_"+q)
		if bodyAt(t, st, "qty") != q {
			t.Fatalf("the probe adds %s:\n%s", q, text)
		}
	}
	wantExpect(t, planStep(t, p, "add_stock_qty_large_1250"), "qty_on_hand", int64(1251))
	wantExpect(t, planStep(t, p, "add_stock_qty_large_12345"), "qty_on_hand", int64(13596))
	if bodyAt(t, planStep(t, p, "add_stock_qty_large_1250"), "id_product") == bodyAt(t, planStep(t, p, "add_stock"), "id_product") {
		t.Fatalf("the magnitude probes run on a product of their own, so a cap fails them only:\n%s", text)
	}

	p, text, _ = shopDemoPlanWith(t, contract.PlanOptions{}, "AddStockBatch")
	batch := planStep(t, p, "add_stock_batch_qty_large")
	if bodyAt(t, batch, "lines.0.qty") != "1250" || bodyAt(t, batch, "lines.1.qty") != "12345" {
		t.Fatalf("each line adds a large quantity:\n%s", text)
	}
	wantExpect(t, batch, "results.0.qty_on_hand", int64(1250))
	wantExpect(t, batch, "results.1.qty_on_hand", int64(12345))
}

func TestAnEmailFailureWordedWithoutTheAtCharacterStillGetsAMalformedProbe(t *testing.T) {
	for _, when := range []string{"email does not contain an at sign", "the email lacks @", "an email without an @", "email has no @ symbol", "the email is missing its at-sign"} {
		p, text := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
			if c := rpcs["shop.customers.v1.CustomerService/CreateCustomer"]; c != nil {
				for i := range c.Failures {
					if c.Failures[i].Reason == "EmailInvalid" {
						c.Failures[i].When = when
					}
				}
			}
		}, "CreateCustomer")
		probe := planStep(t, p, "create_customer_email_no_at")
		if strings.Contains(bodyAt(t, probe, "email"), "@") {
			t.Fatalf("%q: the probe sends an email with no @:\n%s", when, text)
		}
	}
}

func TestAnInvalidArgumentClauseThePlanCannotReadIsNamedInANote(t *testing.T) {
	p, _ := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.customers.v1.CustomerService/CreateCustomer"]; c != nil {
			for i := range c.Failures {
				if c.Failures[i].Reason == "EmailInvalid" {
					c.Failures[i].When = "email is empty, or email is not a well-formed address"
				}
			}
		}
	}, "CreateCustomer")
	planStep(t, p, "create_customer_email_empty")
	notes := strings.Join(p.Notes, "\n")
	if !strings.Contains(notes, "email is not a well-formed address") || !strings.Contains(notes, "EmailInvalid") || !strings.Contains(notes, "at sign") {
		t.Fatalf("a note names the clause no probe was built for, the failure and the wording read:\n%s", notes)
	}
}

func TestAListInCreationOrderAssertsEachFixtureByIdAtItsPosition(t *testing.T) {
	for _, summary := range []string{"List a customer's orders oldest first.", "List a customer's orders in creation order."} {
		p, text := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
			if c := rpcs["shop.orders.v1.OrderService/ListOrders"]; c != nil {
				c.Summary = summary
				c.Exports = nil
			}
		}, "ListOrders")
		list := planStep(t, p, "list_orders")
		for i, id := range []string{"create_order", "create_order_2", "create_order_3"} {
			wantExpect(t, list, "orders."+string(rune('0'+i))+".id_order", "${"+id+".order.id_order}")
		}
		wantExists(t, list, "orders.3", false)
		if strings.Contains(text, "exists: true") && strings.Contains(strings.Join(p.Notes, "\n"), "no scalar field shrt could vary") {
			t.Fatalf("%q: creation order is stated, so the positions are asserted:\n%s", summary, text)
		}
	}
}

func TestAListWithNoStatedOrderAssertsEachFixtureIsAMember(t *testing.T) {
	p, text := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.orders.v1.OrderService/ListOrders"]; c != nil {
			c.Summary = "List a customer's orders."
			c.Exports = nil
		}
	}, "ListOrders")
	list := planStep(t, p, "list_orders")
	found := map[string]bool{}
	for _, e := range list.Expect {
		if m, ok := e.Includes.(map[string]any); ok && e.Path == "orders" {
			found[m["id_order"].(string)] = true
		}
		if strings.HasPrefix(e.Path, "orders.0.") {
			t.Fatalf("no order is stated, so no position is asserted:\n%s", text)
		}
	}
	for _, id := range []string{"create_order", "create_order_2", "create_order_3"} {
		if !found["${"+id+".order.id_order}"] {
			t.Fatalf("list_orders includes %s by id:\n%s", id, text)
		}
	}
	wantExists(t, list, "orders.3", false)
}

func TestAListWhoseContractSaysAnEmptyFilterListsAllIsProbedEmpty(t *testing.T) {
	p, text, _ := shopDemoPlanWith(t, contract.PlanOptions{}, "ListProducts")
	probe := planStep(t, p, "list_products_empty_sku_prefix")
	if bodyAt(t, probe, "sku_prefix") != "" {
		t.Fatalf("the probe sends the prefix empty:\n%s", text)
	}
	found := map[string]bool{}
	for _, e := range probe.Expect {
		if m, ok := e.Includes.(map[string]any); ok && e.Path == "products" {
			found[m["id_product"].(string)] = true
		}
		if strings.HasPrefix(e.Path, "products.0.") {
			t.Fatalf("other runs' products share an unfiltered list, so no position is asserted:\n%s", text)
		}
	}
	for _, id := range []string{"create_product", "create_product_2", "create_product_3", "create_product_prefix_inside"} {
		if !found["${"+id+".product.id_product}"] {
			t.Fatalf("the empty-prefix list includes %s by id:\n%s", id, text)
		}
	}
	wantExists(t, probe, "products.4", true)
}

func TestEmptyFilterGapsNamesAFilterEveryChainSendsSet(t *testing.T) {
	cat, lib := shopDemo(t)
	set := &chain.Chain{Name: "set", Steps: []*chain.Step{{ID: "list_products", Call: "shop.catalog.v1.ProductService/ListProducts", Body: map[string]any{"sku_prefix": "sku-"}}}}
	got := contract.EmptyFilterGaps([]*chain.Chain{set}, lib, cat)
	if len(got) != 1 || got[0].Field != "sku_prefix" || got[0].Chains[0] != "set" {
		t.Fatalf("got %+v, want ListProducts sku_prefix never sent empty", got)
	}
	empty := &chain.Chain{Name: "empty", Steps: []*chain.Step{{ID: "list_products", Call: "shop.catalog.v1.ProductService/ListProducts", Body: map[string]any{"sku_prefix": ""}}}}
	if got := contract.EmptyFilterGaps([]*chain.Chain{set, empty}, lib, cat); len(got) != 0 {
		t.Fatalf("a chain sends it empty: %+v", got)
	}
}

func TestAnUnfilteredListAfterTheStateMovesAssertsEveryFixtureInItsState(t *testing.T) {
	p, text, _ := shopDemoPlanWith(t, contract.PlanOptions{}, "ListOrders")
	after := planStep(t, p, "list_orders_after_moves")
	if bodyAt(t, after, "status") != "ORDER_STATUS_UNSPECIFIED" {
		t.Fatalf("the list after the moves is unfiltered:\n%s", text)
	}
	for _, id := range []string{"cancel_order_2", "confirm_order_3"} {
		if stepIndex(p.Chain, after.ID) < stepIndex(p.Chain, id) {
			t.Fatalf("the unfiltered list runs after %s: %s", id, strings.Join(stepIDs(p), ", "))
		}
	}
	wantExpect(t, after, "orders.1.id_order", "${create_order_2.order.id_order}")
	wantExpect(t, after, "orders.1.status", "ORDER_STATUS_CANCELLED")
	wantExpect(t, after, "orders.2.id_order", "${create_order_3.order.id_order}")
	wantExpect(t, after, "orders.2.status", "ORDER_STATUS_CONFIRMED")
	wantExists(t, after, "orders.3", false)
	for _, s := range p.Chain.Steps {
		if strings.HasPrefix(s.ID, "list_orders_pending") && stepIndex(p.Chain, s.ID) < stepIndex(p.Chain, after.ID) {
			t.Fatalf("the unfiltered list does not depend on the filtered ones: %s", strings.Join(stepIDs(p), ", "))
		}
	}
	for _, ref := range after.References() {
		if strings.Contains(ref, "list_orders_") {
			t.Fatalf("the unfiltered list reads no filtered step: %v", after.References())
		}
	}
}
