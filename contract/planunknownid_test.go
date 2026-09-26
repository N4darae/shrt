package contract_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestPlanProbesEachIDTakingRPCWithAnIDNoRecordHas(t *testing.T) {
	for _, c := range []struct {
		target, step, field, ref, reason string
	}{
		{"GetProduct", "get_product_unknown_id_product", "id_product", "${create_product.product.id_product}", "ProductNotFound"},
		{"GetCustomer", "get_customer_unknown_id_customer", "id_customer", "${create_customer.customer.id_customer}", "CustomerNotFound"},
		{"AddStock", "add_stock_unknown_id_product", "id_product", "${create_product.product.id_product}", "ProductNotFound"},
		{"FetchOrder", "fetch_order_unknown_id_order", "id_order", "${create_order.order.id_order}", "OrderNotFound"},
	} {
		p, text, notes := shopDemoPlan(t, c.target)
		probe := planStep(t, p, c.step)
		if got := bodyAt(t, probe, c.field); !realIDMadeUnknown(got, c.ref) {
			t.Fatalf("%s: the probe sends a real id made unknown, got %s:\n%s", c.target, got, text)
		}
		wantExpect(t, probe, "status.details.0.reason", c.reason)
		if !strings.Contains(notes, c.step) {
			t.Fatalf("%s: the plan names the unknown-id probe:\n%s", c.target, notes)
		}
	}
	p, text, _ := shopDemoPlan(t, "ListOrders")
	if _, ok := p.Chain.Step("list_orders_unknown_id_customer"); ok {
		t.Fatalf("ListOrders declares no not-found failure for id_customer (checked_by: none), so no probe is guessed:\n%s", text)
	}
}

func realIDMadeUnknown(got, ref string) bool {
	if got == ref+"-unknown" {
		return true
	}
	producer, path, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"), ".")
	return ok && got == "${"+producer+"_for_unknown."+path+"}-unknown"
}

func TestPlanSendsEachUnknownIDOnceWithReadsAroundAWrite(t *testing.T) {
	for _, targets := range [][]string{
		{"GetCustomer"},
		{"CreateOrder", "ConfirmOrder", "CancelOrder", "FetchOrder", "ListOrders"},
		{"CancelOrder", "AddStock"},
	} {
		p, text, _ := shopDemoPlan(t, targets...)
		seen := map[string]bool{}
		probed := map[string]string{}
		for _, st := range p.Chain.Steps {
			if seen[st.ID] {
				t.Fatalf("%v: step id %s planned twice:\n%s", targets, st.ID, text)
			}
			seen[st.ID] = true
			if strings.HasSuffix(st.ID, "_unknown_refs") {
				continue
			}
			for _, path := range unknownPaths(st.Body, "") {
				key := st.Call + " " + path
				if other, dup := probed[key]; dup {
					t.Fatalf("%v: %s and %s both send an unknown %s to %s", targets, other, st.ID, path, st.Call)
				}
				probed[key] = st.ID
			}
		}
	}
	p, _, notes := shopDemoPlan(t, "AddStock")
	planStep(t, p, "get_product_before_add_stock_unknown_id_product")
	after := planStep(t, p, "get_product_after_add_stock_unknown_id_product")
	wantExpect(t, after, "product.qty_on_hand", "${get_product_before_add_stock_unknown_id_product.product.qty_on_hand}")
	if strings.Count(notes, "add_stock_unknown_id_product (") != 1 {
		t.Fatalf("one note names the one unknown-id probe:\n%s", notes)
	}
}

func unknownPaths(v any, at string) []string {
	out := []string{}
	switch x := v.(type) {
	case map[string]any:
		for k, sub := range x {
			out = append(out, unknownPaths(sub, strings.TrimPrefix(at+"."+k, "."))...)
		}
	case []any:
		for i, sub := range x {
			out = append(out, unknownPaths(sub, fmt.Sprintf("%s.%d", at, i))...)
		}
	case string:
		if strings.HasSuffix(x, "-unknown") || strings.HasPrefix(x, "no-such-") {
			out = append(out, at)
		}
	}
	return out
}

func TestPlanSendsAnUnknownIDOnTheFirstLineOfARepeatedFieldToo(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateOrder")
	last := planStep(t, p, "create_order_unknown_id_product")
	first := planStep(t, p, "create_order_unknown_id_product_first_line")
	if got := bodyAt(t, last, "lines.0.id_product"); strings.HasSuffix(got, "-unknown") {
		t.Fatalf("the last-line probe keeps its first line real, got %s:\n%s", got, text)
	}
	if got := bodyAt(t, first, "lines.0.id_product"); !strings.HasSuffix(got, "-unknown") {
		t.Fatalf("the first-line probe sends the unknown id on line 0, got %s:\n%s", got, text)
	}
	if got := bodyAt(t, first, "lines.1.id_product"); strings.HasSuffix(got, "-unknown") {
		t.Fatalf("the first-line probe keeps its last line real, got %s:\n%s", got, text)
	}
	wantExpect(t, first, "status.details.0.reason", "ProductNotFound")
	planStep(t, p, "get_product_after_create_order_unknown_id_product_first_line")
	if !strings.Contains(notes, "create_order_unknown_id_product_first_line (lines.0.id_product") {
		t.Fatalf("the plan names the first-line probe:\n%s", notes)
	}
}
