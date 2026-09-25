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
