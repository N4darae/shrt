package contract_test

import (
	"strings"
	"testing"
)

func TestPlanRunsAStateChangingTargetOnOneAndThreeItemFixtures(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CancelOrder")
	one := planStep(t, p, "create_order_1_lines")
	if got := bodyAt(t, one, "lines.0.id_product"); got != "${create_product.product.id_product}" {
		t.Fatalf("the one-item fixture keeps the first item, got %s:\n%s", got, text)
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
