package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestPlanGivesEverySucceedingVariantTheMainStepsStateAndReadBack(t *testing.T) {
	p, text, notes := shopDemoPlanWith(t, contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}}, "ConfirmOrder")
	wantExpect(t, planStep(t, p, "fetch_order_after_confirm_order"), "order.status", "ORDER_STATUS_CONFIRMED")
	for _, id := range []string{"confirm_order_3_lines", "confirm_order_1_lines", "confirm_order_as_clerk", "confirm_order_exact_stock", "confirm_order_same_product_twice"} {
		wantExpect(t, planStep(t, p, id), "order.status", "ORDER_STATUS_CONFIRMED")
	}
	read := planStep(t, p, "fetch_order_after_confirm_order_3_lines")
	if got := bodyAt(t, read, "id_order"); got != "${create_order_3_lines.order.id_order}" {
		t.Fatalf("the read-back reads the 3-line order, got %s:\n%s", got, text)
	}
	wantExpect(t, read, "order.status", "ORDER_STATUS_CONFIRMED")
	wantExpect(t, read, "order.lines.2.qty", "${steps.create_order_3_lines.request.lines.2.qty}")
	if stepIndex(p.Chain, read.ID) < stepIndex(p.Chain, "confirm_order_3_lines") {
		t.Fatalf("the read comes after the write: %s", strings.Join(stepIDs(p), ", "))
	}
	for _, id := range []string{"fetch_order_after_confirm_order_exact_stock", "fetch_order_after_confirm_order_same_product_twice"} {
		wantExpect(t, planStep(t, p, id), "order.status", "ORDER_STATUS_CONFIRMED")
	}
	if _, ok := p.Chain.Step("fetch_order_after_create_order_3_lines"); ok {
		t.Fatalf("a fixture the target then acts on gets no read of its own:\n%s", strings.Join(stepIDs(p), ", "))
	}
	if !strings.Contains(notes, "fetch_order_after_confirm_order_3_lines") {
		t.Fatalf("a note names the added reads:\n%s", notes)
	}
}

func TestPlanReadsBackACreatesVariantsButNotItsReplays(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "CreateOrder")
	wantExpect(t, planStep(t, p, "fetch_order_after_create_order"), "order.status", "ORDER_STATUS_PENDING")
	wantExpect(t, planStep(t, p, "fetch_order_after_create_order_3_lines"), "order.status", "ORDER_STATUS_PENDING")
	for _, id := range []string{"fetch_order_after_create_order_replay", "fetch_order_after_create_order_no_key"} {
		if _, ok := p.Chain.Step(id); ok {
			t.Fatalf("an idempotency replay is not read back again, found %s:\n%s", id, text)
		}
	}
}

func TestPlanSendsALargeValueForANumberInsideARepeatedItem(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateOrder")
	large := planStep(t, p, "create_order_qty_large")
	if got := bodyAt(t, large, "lines.0.qty"); got != "12345" {
		t.Fatalf("the first line carries the large quantity, got %s:\n%s", got, text)
	}
	if got := bodyAt(t, large, "lines.1.qty"); got == "12345" {
		t.Fatalf("the other lines stay normal:\n%s", text)
	}
	wantExpect(t, large, "status.code", "SUCCESS")
	wantExpect(t, planStep(t, p, "fetch_order_after_create_order_qty_large"), "order.lines.0.qty", "${steps.create_order_qty_large.request.lines.0.qty}")
	if !strings.Contains(notes, "create_order_qty_large (lines.0.qty = 12345") {
		t.Fatalf("the boundary note names the probe:\n%s", notes)
	}
	c, _, _ := shopDemoPlan(t, "ConfirmOrder")
	if _, ok := c.Chain.Step("create_order_qty_large"); ok {
		t.Fatal("only the target gets the probe")
	}
}

func TestPlanAssertsTheStreamsFirstMessageCarriesTheRequestedRecord(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "WatchOrder")
	st := planStep(t, p, "watch_order")
	wantExpect(t, st, "messages.0.status.code", "SUCCESS")
	wantExpect(t, st, "messages.0.order.id_order", "${create_order.order.id_order}")
	wantExpect(t, st, "messages.0.order.lines.0.qty", "${steps.create_order.request.lines.0.qty}")
	for _, e := range st.Expect {
		if !strings.HasPrefix(e.Path, "messages.0.") {
			t.Fatalf("a streaming step's paths start at messages.N:\n%s", text)
		}
	}
}
