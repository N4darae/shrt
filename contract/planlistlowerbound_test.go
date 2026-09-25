package contract_test

import (
	"strings"
	"testing"
)

func TestAListAssertedToHoldExactlyNItemsAssertsTheNthItemExistsToo(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateOrder", "ConfirmOrder", "CancelOrder", "FetchOrder", "ListOrders")
	list := planStep(t, p, "list_orders")
	wantExists(t, list, "orders.3", false)
	wantExists(t, list, "orders.2", true)
	if !strings.Contains(notes, "holds exactly 3 item(s)") {
		t.Fatalf("the note that promises an exact count is backed by both bounds:\n%s\n%s", notes, text)
	}
}
