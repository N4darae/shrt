package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func cancelPlan(t *testing.T, from string) *contract.Plan {
	t.Helper()
	cat, _ := shopDemo(t)
	lib := shopDemoEdited(t, func(name, body string) string {
		head := "    shop.orders.v1.OrderService/CancelOrder:\n"
		i := strings.Index(body, head)
		if i < 0 {
			return body
		}
		rest := strings.Replace(body[i+len(head):], "from: shop.orders.v1.OrderService/CreateOrder->order.id_order", "from: "+from, 1)
		return body[:i] + head + "        effects: {qty_on_hand: {restore: ORDER_STATUS_CONFIRMED}}\n" + rest
	})
	p, err := contract.BuildPlanFor([]string{"CancelOrder"}, lib, cat, "shopdemo")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestARestoreNoProbeReadsIsAGapNamingTheWiring(t *testing.T) {
	p := cancelPlan(t, "shop.orders.v1.OrderService/ConfirmOrder->order.id_order")
	gaps := strings.Join(p.GapNotes(), "\n")
	for _, want := range []string{
		"step cancel_order: no probe reads the qty_on_hand it gives back from ORDER_STATUS_CONFIRMED",
		"id_order comes from ConfirmOrder, which already leaves the order CONFIRMED",
		"set its from: to shop.orders.v1.OrderService/CreateOrder->order.id_order with needs: [shop.orders.v1.OrderService/ConfirmOrder]",
	} {
		if !strings.Contains(gaps, want) {
			t.Fatalf("want gap %q in:\n%s", want, gaps)
		}
	}

	p = cancelPlan(t, "shop.orders.v1.OrderService/CreateOrder->order.id_order")
	if _, ok := p.Chain.Step("get_product_after_cancel_order_after_confirmed"); !ok {
		t.Fatalf("wired from CreateOrder, the restore is read after a confirmed order is cancelled")
	}
	if gaps := strings.Join(p.GapNotes(), "\n"); strings.Contains(gaps, "no probe reads the qty_on_hand") {
		t.Fatalf("a restore that is read is no gap:\n%s", gaps)
	}
}
