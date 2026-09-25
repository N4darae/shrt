package contract_test

import (
	"strings"
	"testing"
)

func TestAListFilterStateWhoseProducerNeedsAnUncalledRpcIsNotCalledUnreachable(t *testing.T) {
	_, notes := confirmNeedsStockPlan(t, "ListOrders")
	if !strings.Contains(notes, "ConfirmOrder would move a fixture to CONFIRMED, but it needs AddStock") {
		t.Fatalf("fixture: ConfirmOrder needs AddStock, which a plan of ListOrders alone does not call:\n%s", notes)
	}
	if strings.Contains(notes, "No producer in the contracts reaches ORDER_STATUS_CONFIRMED") {
		t.Fatalf("ConfirmOrder produces CONFIRMED, so the notes must not say no producer reaches it:\n%s", notes)
	}
	if !strings.Contains(notes, "The filter on ORDER_STATUS_CONFIRMED is not probed either: ConfirmOrder reaches it but needs AddStock") {
		t.Fatalf("the filter note must say why CONFIRMED is not probed:\n%s", notes)
	}
}
