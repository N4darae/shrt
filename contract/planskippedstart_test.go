package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

const restoreConfirmed = "        effects: {qty_on_hand: {restore: CONFIRMED}}\n"

func cancelNeedsConfirm(extra, failure string) func(name, body string) string {
	return func(name, body string) string {
		head := "    shop.orders.v1.OrderService/CancelOrder:\n"
		i := strings.Index(body, head)
		if i < 0 {
			return body
		}
		rest := body[i+len(head):]
		if failure != "" {
			rest = strings.Replace(rest, "              when: the order is already CANCELLED\n", "              when: the order is already CANCELLED\n"+failure, 1)
		}
		return body[:i] + head + "        needs: [shop.orders.v1.OrderService/ConfirmOrder]\n" + extra + rest
	}
}

func confirmedFirst(p *contract.Plan, cancel *chain.Step) bool {
	order, _ := cancel.Body["id_order"].(string)
	head, _, _ := strings.Cut(strings.TrimPrefix(order, "${"), ".")
	for _, st := range p.Chain.Steps {
		if st == cancel {
			return false
		}
		if strings.HasSuffix(st.Call, "/ConfirmOrder") && strings.Contains(st.Body["id_order"].(string), "${"+head+".") {
			return true
		}
	}
	return false
}

func TestANeedThatOnlyReachesTheStateARestoreNamesLeavesTheStateBeforeItPlanned(t *testing.T) {
	p := editedPlan(t, cancelNeedsConfirm(restoreConfirmed, ""), "CancelOrder")
	for _, id := range []string{"cancel_order", "cancel_order_1_lines", "cancel_order_3_lines", "cancel_order_after_confirmed"} {
		st, ok := p.Chain.Step(id)
		if !ok || !confirmedFirst(p, st) {
			t.Fatalf("%s still runs on a confirmed order (found %v)", id, ok)
		}
	}
	for _, id := range []string{"cancel_order_from_pending", "cancel_order_1_lines_from_pending", "cancel_order_3_lines_from_pending"} {
		st, ok := p.Chain.Step(id)
		if !ok {
			t.Fatalf("no step %s cancels a PENDING order", id)
		}
		if confirmedFirst(p, st) {
			t.Fatalf("%s must cancel an order no ConfirmOrder reached", id)
		}
		if !hasExpect(st, "status.code", "SUCCESS") || !hasExpect(st, "order.status", "ORDER_STATUS_CANCELLED") {
			t.Fatalf("%s expects the cancel to succeed: %+v", id, st.Expect)
		}
	}
	three, _ := p.Chain.Step("cancel_order_3_lines_from_pending")
	src, _, _ := strings.Cut(strings.TrimPrefix(three.Body["id_order"].(string), "${"), ".")
	if fixture, ok := p.Chain.Step(src); !ok || len(fixture.Body["lines"].([]any)) != 3 {
		t.Fatalf("cancel_order_3_lines_from_pending acts on an order of 3 lines, not %s", src)
	}
	after, ok := p.Chain.Step("get_product_after_cancel_order_from_pending")
	if !ok || !hasExpect(after, "product.qty_on_hand", "${get_product_before_cancel_order_from_pending.product.qty_on_hand}") {
		t.Fatalf("the stock after cancelling a PENDING order is what it was before it, as nothing was taken")
	}
	notes := strings.Join(p.Notes, "\n")
	if !strings.Contains(notes, "needs: [ConfirmOrder] only takes the order to CONFIRMED, where its restore: applies, so it acts on a PENDING order too") {
		t.Fatalf("a note says why the PENDING order is planned:\n%s", notes)
	}
}

func TestANeedThatIsAPreconditionKeepsEveryCallAfterIt(t *testing.T) {
	for _, tc := range []struct{ name, extra, failure string }{
		{"no effect names the state the need reaches", "", ""},
		{"a restore names a state the need does not reach", "        effects: {qty_on_hand: {restore: CANCELLED}}\n", ""},
		{"the state before the need is refused", restoreConfirmed,
			"            - code: 1306\n              reason: OrderPending\n              when: the order is still PENDING\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := editedPlan(t, cancelNeedsConfirm(tc.extra, tc.failure), "CancelOrder")
			for _, st := range p.Chain.Steps {
				if strings.Contains(st.ID, "_from_") {
					t.Fatalf("step %s calls CancelOrder from a state its contract does not allow", st.ID)
				}
				if strings.HasSuffix(st.Call, "/CancelOrder") && hasExpect(st, "status.code", "SUCCESS") && !confirmedFirst(p, st) {
					t.Fatalf("step %s cancels an order no ConfirmOrder reached", st.ID)
				}
			}
		})
	}
}

func TestStateGapsNameTheStateAndItemCountsNoChainCovers(t *testing.T) {
	cat, lib := shopDemoEdited(t, cancelNeedsConfirm(restoreConfirmed, ""))
	p, err := contract.BuildPlanFor([]string{"CancelOrder"}, lib, cat, "orders-cancelorder")
	if err != nil {
		t.Fatal(err)
	}
	plans := map[string]*contract.Plan{"shop.orders.v1.OrderService/CancelOrder": p}
	if gaps := contract.StateGaps([]*chain.Chain{p.Chain}, plans, lib, cat); len(gaps) != 0 {
		t.Fatalf("the plan's own chain leaves no state gap: %+v", gaps)
	}
	old := &chain.Chain{Name: "orders-cancelorder"}
	hand := &chain.Chain{Name: "edge-flows"}
	for _, st := range p.Chain.Steps {
		if !strings.Contains(st.ID, "from_pending") {
			old.Steps = append(old.Steps, st)
		}
		if st.ID == "create_order_for_cancel_order_from_pending" || st.ID == "cancel_order_from_pending" {
			hand.Steps = append(hand.Steps, st)
		}
	}
	sits := contract.Situations(hand, lib, cat)
	if got := sits["cancel_order_from_pending"].String(); got != "on an order in PENDING, lines of 2 items" {
		t.Fatalf("the hand chain cancels a PENDING order of 2 lines, got %q", got)
	}
	if got := contract.Situations(old, lib, cat)["cancel_order_3_lines"].String(); got != "on an order in CONFIRMED, lines of 3 items" {
		t.Fatalf("the planned 3-line cancel runs on a CONFIRMED order, got %q", got)
	}
	gaps := contract.StateGaps([]*chain.Chain{old}, plans, lib, cat)
	if len(gaps) != 1 || gaps[0].State != "PENDING" || len(gaps[0].Missing) != 3 || len(gaps[0].Sent) != 0 {
		t.Fatalf("a PENDING order is never cancelled: %+v", gaps)
	}
	if line := gaps[0].Line(); line != "CancelOrder on a PENDING order: no chain calls it so; its plan sends 1, 2 or 3 lines" || !strings.HasPrefix(gaps[0].Why, "needs: [ConfirmOrder]") {
		t.Fatalf("the gap names the state and what the plan sends there in plain words, and keeps why for contract status -gaps: %s (%s)", line, gaps[0].Why)
	}
	gaps = contract.StateGaps([]*chain.Chain{old, hand}, plans, lib, cat)
	if len(gaps) != 1 || gaps[0].Line() != "CancelOrder on a PENDING order: no chain sends 1 or 3 lines (edge-flows sends 2)" {
		t.Fatalf("a hand chain cancelling a 2-line PENDING order leaves 1 and 3 lines: %+v", gaps)
	}
}

func hasExpect(st *chain.Step, path string, want any) bool {
	for _, e := range st.Expect {
		if e.Path == path && e.Equals == want {
			return true
		}
	}
	return false
}
