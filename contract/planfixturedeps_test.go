package contract_test

import (
	"strings"
	"testing"

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
