package contract_test

import (
	"strconv"
	"strings"
	"testing"
)

func idAt(t *testing.T, ids []string, id string) int {
	t.Helper()
	for i, x := range ids {
		if x == id {
			return i
		}
	}
	t.Fatalf("no step %s among %s", id, strings.Join(ids, ", "))
	return -1
}

func oneMore(t *testing.T, v string) string {
	t.Helper()
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("not a number: %q", v)
	}
	return strconv.Itoa(n + 1)
}

func TestTheShortageAsksForOneMoreThanTheChainAddedSoACapOnQuantityDoesNotMaskIt(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "ConfirmOrder")
	for _, id := range []string{"create_order_for_insufficient_stock", "create_order_for_insufficient_stock_last_item"} {
		st := planStep(t, p, id)
		for _, path := range []string{"lines.0.qty", "lines.1.qty"} {
			n, err := strconv.Atoi(bodyAt(t, st, path))
			if err != nil || n > 99 {
				t.Fatalf("%s %s must stay small, below a cap such as 99 a backend may put on quantities, got %s:\n%s", id, path, bodyAt(t, st, path), text)
			}
		}
	}
	if strings.Contains(text, "100000") || !strings.Contains(notes, "one more than the stock") {
		t.Fatalf("the plan derives the shortage from the stock it set up and says so:\n%s", notes)
	}
}

func TestPlanForAnInsufficiencyRefusalProvesTheRefusedWriteChangedNothing(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "ConfirmOrder")
	short := planStep(t, p, "create_order_for_insufficient_stock")
	if got, want := bodyAt(t, short, "lines.0.qty"), oneMore(t, bodyAt(t, planStep(t, p, "add_stock"), "qty")); got != want {
		t.Fatalf("the first line asks for one more than the stock add_stock added, %s, got %s:\n%s", want, got, text)
	}
	if got := bodyAt(t, short, "lines.1.qty"); got != bodyAt(t, planStep(t, p, "create_order"), "lines.1.qty") {
		t.Fatalf("the other line keeps a quantity that is in stock, got %s:\n%s", got, text)
	}
	last := planStep(t, p, "create_order_for_insufficient_stock_last_item")
	if got := bodyAt(t, last, "lines.1.qty"); got != oneMore(t, bodyAt(t, planStep(t, p, "add_stock_2"), "qty")) {
		t.Fatalf("a second probe puts the shortage on the last line, which a backend checking only the first misses, got %s:\n%s", got, text)
	}
	planStep(t, p, "fetch_order_after_confirm_order_insufficient_stock_last_item")
	refused := planStep(t, p, "confirm_order_insufficient_stock")
	if got := bodyAt(t, refused, "id_order"); got != "${create_order_for_insufficient_stock.order.id_order}" {
		t.Fatalf("the refused confirm reads the short order, got %s:\n%s", got, text)
	}
	wantExpect(t, refused, "status.details.0.reason", "InsufficientStock")
	wantExpect(t, refused, "status.details.0.app_code", 1305)

	ids := stepIDs(p)
	at := idAt(t, ids, refused.ID)
	for _, pair := range [][3]string{
		{"get_product", "product.qty_on_hand", ""},
		{"get_product_2", "product.qty_on_hand", ""},
		{"fetch_order", "order.status", ""},
	} {
		before := pair[0] + "_before_" + refused.ID
		after := pair[0] + "_after_" + refused.ID
		if idAt(t, ids, before) > at || idAt(t, ids, after) < at {
			t.Fatalf("%s reads before the refusal and %s after it: %s", before, after, strings.Join(ids, ", "))
		}
		wantExpect(t, planStep(t, p, after), pair[1], "${"+before+"."+pair[1]+"}")
	}
	if !strings.Contains(notes, "confirm_order_insufficient_stock") || !strings.Contains(notes, "unchanged") {
		t.Fatalf("the plan says what the refusal probe proves: %s", notes)
	}
}

func TestAShortageSplitOverTwoItemsOfOneResourceIsRefusedWhenOnlyTheSumExceedsTheStock(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "ConfirmOrder")
	split := planStep(t, p, "create_order_for_insufficient_stock_split")
	if a, b := bodyAt(t, split, "lines.0.id_product"), bodyAt(t, split, "lines.1.id_product"); a != b {
		t.Fatalf("both lines name one product: %s %s\n%s", a, b, text)
	}
	stock, _ := strconv.Atoi(bodyAt(t, planStep(t, p, "add_stock"), "qty"))
	a, _ := strconv.Atoi(bodyAt(t, split, "lines.0.qty"))
	b, _ := strconv.Atoi(bodyAt(t, split, "lines.1.qty"))
	if a > stock || b > stock || a+b != stock+1 {
		t.Fatalf("each line fits the stock of %d and only their sum does not, got %d and %d:\n%s", stock, a, b, text)
	}
	refused := planStep(t, p, "confirm_order_insufficient_stock_split")
	wantExpect(t, refused, "status.details.0.reason", "InsufficientStock")
	planStep(t, p, "get_product_after_confirm_order_insufficient_stock_split")
}
