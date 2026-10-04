package contract_test

import (
	"strings"
	"testing"
)

func TestADeclaredEffectThePlanDoesNotAssertIsAGap(t *testing.T) {
	for _, tc := range []struct {
		name, from, effect, step, gap string
	}{
		{"id from the rpc that already confirms", "ConfirmOrder", "{restore: CONFIRMED}", "get_product_after_cancel_order_after_confirmed", ""},
		{"id from the rpc that creates", "CreateOrder", "{restore: CONFIRMED}", "get_product_after_cancel_order_after_confirmed", ""},
		{"a state no probe moves it to first", "CreateOrder", "{restore: PENDING}", "",
			"step cancel_order: no step asserts effects: {qty_on_hand: {restore: PENDING}}, so a CancelOrder that breaks it passes; no probe moves a fresh order to PENDING before cancel_order acts on it: take id_order from: the rpc that creates the order"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := editedPlan(t, func(name, body string) string {
				head := "    shop.orders.v1.OrderService/CancelOrder:\n"
				i := strings.Index(body, head)
				if i < 0 {
					return body
				}
				rest := strings.Replace(body[i+len(head):], "OrderService/CreateOrder->order.id_order", "OrderService/"+tc.from+"->order.id_order", 1)
				return body[:i] + head + "        effects: {qty_on_hand: " + tc.effect + "}\n" + rest
			}, "CancelOrder")
			gaps := strings.Join(p.GapNotes(), "\n")
			if tc.gap == "" && strings.Contains(gaps, "no step asserts") {
				t.Fatalf("an asserted effect is no gap:\n%s", gaps)
			}
			if tc.gap != "" && !strings.Contains(gaps, tc.gap) {
				t.Fatalf("want gap %q in:\n%s", tc.gap, gaps)
			}
			if tc.step == "" {
				return
			}
			st, ok := p.Chain.Step(tc.step)
			if !ok {
				t.Fatalf("no step %s reads the stock back", tc.step)
			}
			for _, e := range st.Expect {
				if e.Path == "product.qty_on_hand" && strings.Contains(e.Equals.(string), "_before_confirm_order_before_cancel_order_after_confirmed.") {
					return
				}
			}
			t.Fatalf("%s does not assert the stock is back: %+v", tc.step, st.Expect)
		})
	}
}
