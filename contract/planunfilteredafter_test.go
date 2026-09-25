package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestTheListAfterTheMovesSendsNoFilterEvenWhenTheContractGivesTheFilterAValue(t *testing.T) {
	cat, _ := shopDemo(t)
	lib := shopDemoEdited(t, func(name, body string) string {
		if name != "orders.yaml" {
			return body
		}
		return strings.Replace(body, "            status:\n                note: ORDER_STATUS_UNSPECIFIED means no filter; any other value keeps only orders in that status\n",
			"            status:\n                value: ORDER_STATUS_PENDING\n                note: ORDER_STATUS_UNSPECIFIED means no filter; any other value keeps only orders in that status\n", 1)
	})
	p, err := contract.BuildPlanFor([]string{"ListOrders"}, lib, cat, "shopdemo")
	if err != nil {
		t.Fatal(err)
	}
	after := planStep(t, p, "list_orders_after_moves")
	for k := range after.Body {
		if strings.EqualFold(k, "status") {
			raw, _ := p.YAML()
			t.Fatalf("the list described as having no filter sends none, got %s=%v:\n%s", k, after.Body[k], raw)
		}
	}
}
