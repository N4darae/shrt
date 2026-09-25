package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestPlanReadsAnOpenReadAsEachProfileAndAssertsTheSameAnswer(t *testing.T) {
	p, text, notes := shopDemoPlanWith(t, contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}}, "GetProduct")
	st := planStep(t, p, "get_product_as_clerk")
	if st.Auth != "clerk" {
		t.Fatalf("the parity read runs as clerk, got %q:\n%s", st.Auth, text)
	}
	for _, f := range []string{"id_product", "sku", "name", "price_minor", "qty_on_hand"} {
		wantExpect(t, st, "product."+f, "${get_product.product."+f+"}")
	}
	if stepIndex(p.Chain, "get_product_as_clerk") != stepIndex(p.Chain, "get_product")+1 {
		t.Fatalf("the parity read follows the read it compares with: %s", strings.Join(stepIDs(p), ", "))
	}
	if !strings.Contains(notes, "get_product_as_clerk") {
		t.Fatalf("a note names the parity read: %s", notes)
	}
}

func TestPlanRunsAnOpenWriteAsTheLowerProfileOnItsOwnFixtureAndComparesSideEffects(t *testing.T) {
	p, text, notes := shopDemoPlanWith(t, contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}}, "ConfirmOrder")
	w := planStep(t, p, "confirm_order_as_clerk")
	if w.Auth != "clerk" {
		t.Fatalf("the write runs as clerk, got %q:\n%s", w.Auth, text)
	}
	if got := bodyAt(t, w, "id_order"); got != "${create_order_for_clerk.order.id_order}" {
		t.Fatalf("the clerk confirms its own order, got %s:\n%s", got, text)
	}
	order := planStep(t, p, "create_order_for_clerk")
	if got := bodyAt(t, order, "lines.0.id_product"); got != "${create_product_for_clerk.product.id_product}" {
		t.Fatalf("the clerk's order reads the clerk's product, got %s:\n%s", got, text)
	}
	if got := bodyAt(t, order, "lines.1.id_product"); got != "${create_product_2_for_clerk.product.id_product}" {
		t.Fatalf("the clerk's second line reads the clerk's second product, got %s:\n%s", got, text)
	}
	stock := planStep(t, p, "add_stock_for_clerk")
	if bodyAt(t, stock, "id_product") != "${create_product_for_clerk.product.id_product}" || bodyAt(t, stock, "qty") != bodyAt(t, planStep(t, p, "add_stock"), "qty") {
		t.Fatalf("the clerk's product is stocked as the first one is:\n%s", text)
	}
	prod := planStep(t, p, "create_product_for_clerk")
	if bodyAt(t, prod, "price_minor") != bodyAt(t, planStep(t, p, "create_product"), "price_minor") || bodyAt(t, prod, "sku") == bodyAt(t, planStep(t, p, "create_product"), "sku") {
		t.Fatalf("the clerk's product has the same numbers and its own sku:\n%s", text)
	}
	admin := planStep(t, p, "get_product_after_confirm_order")
	if at := stepIndex(p.Chain, admin.ID) - stepIndex(p.Chain, "confirm_order"); at < 1 || at > 3 {
		t.Fatalf("the admin side effect is read right after the write: %s", strings.Join(stepIDs(p), ", "))
	}
	clerk := planStep(t, p, "get_product_after_confirm_order_as_clerk")
	if got := bodyAt(t, clerk, "id_product"); got != "${create_product_for_clerk.product.id_product}" {
		t.Fatalf("the clerk side effect reads the clerk's product, got %s:\n%s", got, text)
	}
	wantExpect(t, clerk, "product.qty_on_hand", "${get_product_after_confirm_order.product.qty_on_hand}")
	clerk2 := planStep(t, p, "get_product_2_after_confirm_order_as_clerk")
	wantExpect(t, clerk2, "product.qty_on_hand", "${get_product_2_after_confirm_order.product.qty_on_hand}")
	fetch := planStep(t, p, "fetch_order_after_confirm_order_as_clerk")
	wantExpect(t, fetch, "order.status", "${fetch_order_after_confirm_order.order.status}")
	if stepIndex(p.Chain, "confirm_order_as_clerk") > stepIndex(p.Chain, clerk.ID) {
		t.Fatalf("the clerk side effect is read after the clerk write: %s", strings.Join(stepIDs(p), ", "))
	}
	if !strings.Contains(notes, "confirm_order_as_clerk") {
		t.Fatalf("a note names the parity write: %s", notes)
	}
}
