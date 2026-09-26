package contract_test

import (
	"strconv"
	"strings"
	"testing"
)

func TestPlanRunsAStateChangingTargetOnOneAndThreeItemFixtures(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CancelOrder")
	one := planStep(t, p, "create_order_1_lines")
	if got := bodyAt(t, one, "lines.0.id_product"); got != "${create_product_for_items.product.id_product}" {
		t.Fatalf("the one-item fixture keeps the first item, read from the item probes' own product, got %s:\n%s", got, text)
	}
	if n := len(one.Body["lines"].([]any)); n != 1 {
		t.Fatalf("create_order_1_lines sends one line, got %d:\n%s", n, text)
	}
	three := planStep(t, p, "create_order_3_lines")
	if n := len(three.Body["lines"].([]any)); n != 3 {
		t.Fatalf("create_order_3_lines sends three lines, got %d:\n%s", n, text)
	}
	if got := bodyAt(t, three, "lines.2.id_product"); got != "${create_product_3.product.id_product}" {
		t.Fatalf("the third item reads a third product, got %s:\n%s", got, text)
	}
	planStep(t, p, "create_product_3")
	wantExists(t, three, "order.lines.2", true)
	wantExists(t, three, "order.lines.3", false)
	wantExpect(t, planStep(t, p, "cancel_order_3_lines"), "order.id_order", "${create_order_3_lines.order.id_order}")
	for id, fixture := range map[string]string{"cancel_order_1_lines": "create_order_1_lines", "cancel_order_3_lines": "create_order_3_lines"} {
		st := planStep(t, p, id)
		if got := bodyAt(t, st, "id_order"); got != "${"+fixture+".order.id_order}" {
			t.Fatalf("%s cancels %s, got %s:\n%s", id, fixture, got, text)
		}
		wantExpect(t, st, "status.code", "SUCCESS")
	}
	ids := stepIDs(p)
	if stepIndex(p.Chain, "create_order_3_lines") > stepIndex(p.Chain, "cancel_order_3_lines") {
		t.Fatalf("the fixture comes before the target copy: %s", strings.Join(ids, ", "))
	}
	if !strings.Contains(notes, "cancel_order_3_lines") || !strings.Contains(notes, "three") {
		t.Fatalf("a note says why the count is varied: %s", notes)
	}
}

func TestPlanPreparesTheThirdItemsResourceBeforeTheTargetReadsIt(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "ConfirmOrder")
	stock := planStep(t, p, "add_stock_3")
	if got := bodyAt(t, stock, "id_product"); got != "${create_product_3.product.id_product}" {
		t.Fatalf("add_stock_3 stocks the third product, got %s:\n%s", got, text)
	}
	ids := stepIDs(p)
	if stepIndex(p.Chain, "add_stock_3") > stepIndex(p.Chain, "confirm_order_3_lines") {
		t.Fatalf("the third product is stocked before it is confirmed: %s", strings.Join(ids, ", "))
	}
	if stepIndex(p.Chain, "create_product_3") > stepIndex(p.Chain, "add_stock_3") {
		t.Fatalf("the third product exists before it is stocked: %s", strings.Join(ids, ", "))
	}
}

func TestPlanSendsABatchTwelveItemsEachNamingItsOwnResource(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "AddStockBatch")
	many := planStep(t, p, "add_stock_batch_12_lines")
	lines := many.Body["lines"].([]any)
	if len(lines) != 12 {
		t.Fatalf("the many-items probe sends 12 lines, so caps at 5 and 10 both show, got %d:\n%s", len(lines), text)
	}
	seen := map[string]bool{}
	for i := range lines {
		id := bodyAt(t, many, "lines."+strconv.Itoa(i)+".id_product")
		if seen[id] {
			t.Fatalf("each line names a product of its own, %s repeats:\n%s", id, text)
		}
		seen[id] = true
	}
	wantExpect(t, many, "results.11.status.code", "SUCCESS")
	wantExists(t, many, "results.12", false)
	for _, e := range many.Expect {
		if strings.HasPrefix(e.Path, "results.5.") {
			t.Fatalf("only the first and the last line are asserted one by one, got %s:\n%s", e.Path, text)
		}
	}
	wantExpect(t, planStep(t, p, "get_product_after_add_stock_batch_12_lines"), "product.qty_on_hand", "${add_stock_batch_12_lines.results.0.qty_on_hand}")
	wantExpect(t, planStep(t, p, "get_product_12_after_add_stock_batch_12_lines"), "product.qty_on_hand", "${add_stock_batch_12_lines.results.11.qty_on_hand}")
	planStep(t, p, "create_product_12")
	if !strings.Contains(notes, "add_stock_batch_12_lines sends 12") {
		t.Fatalf("a note says why twelve: %s", notes)
	}
	if _, ok := p.Chain.Step("create_order_12_lines"); ok {
		t.Fatalf("one many-items probe per repeated field of the target")
	}
}

func TestPlanKeepsAStateChangingTargetOnAnOrderAtOneAndThreeItems(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "CancelOrder")
	for _, id := range []string{"create_order_12_lines", "cancel_order_12_lines", "create_product_4"} {
		if _, ok := p.Chain.Step(id); ok {
			t.Fatalf("twelve items are sent only where the target's own request repeats them, got %s:\n%s", id, text)
		}
	}
}
