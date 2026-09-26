package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestAListFilterStateWhoseProducerNeedsAnUncalledRpcIsNotCalledUnreachable(t *testing.T) {
	p, _ := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.orders.v1.OrderService/CreateOrder"]; c != nil {
			c.Needs = nil
		}
		if c := rpcs["shop.orders.v1.OrderService/ConfirmOrder"]; c != nil {
			c.Needs = []string{"shop.catalog.v1.StockService/AddStockBatch"}
		}
	}, "ListOrders")
	notes := strings.Join(p.Notes, "\n")
	if !strings.Contains(notes, "ConfirmOrder would move a fixture to CONFIRMED, but it needs AddStockBatch") {
		t.Fatalf("fixture: ConfirmOrder needs AddStockBatch, which a plan of ListOrders alone neither calls nor adds:\n%s", notes)
	}
	if strings.Contains(notes, "No producer in the contracts reaches ORDER_STATUS_CONFIRMED") {
		t.Fatalf("ConfirmOrder produces CONFIRMED, so the notes must not say no producer reaches it:\n%s", notes)
	}
	if !strings.Contains(notes, "The filter on ORDER_STATUS_CONFIRMED is not probed either: ConfirmOrder reaches it but needs AddStockBatch") {
		t.Fatalf("the filter note must say why CONFIRMED is not probed:\n%s", notes)
	}
}

func TestAListFilterStateWhoseProducerNeedsAWriteThePlanCanAddIsProbed(t *testing.T) {
	p, notes := confirmNeedsStockPlan(t, "ListOrders")
	if strings.Contains(notes, "but it needs AddStock") {
		t.Fatalf("AddStock is added as a fixture, so CONFIRMED is not dropped:\n%s", notes)
	}
	planStep(t, p, "list_orders_confirmed")
}
