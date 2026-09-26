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
	if got := bodyAt(t, after, "status"); got != "ORDER_STATUS_UNSPECIFIED" {
		raw, _ := p.YAML()
		t.Fatalf("the list described as having no filter sends the unset value, not the contract's value, got %v:\n%s", got, raw)
	}
}
