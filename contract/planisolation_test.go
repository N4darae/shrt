package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestPlanGivesEachWriteProbeGroupFixturesOfItsOwn(t *testing.T) {
	cat, lib := shopDemo(t)
	p, err := contract.BuildPlanWith([]string{"ConfirmOrder"}, lib, cat, "iso", contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := p.YAML()
	text := string(raw)
	shared := []string{"create_product", "create_product_2", "add_stock", "add_stock_2", "create_customer", "create_order"}
	groups := map[string][]string{
		"shortage": {"create_order_for_insufficient_stock", "confirm_order_insufficient_stock", "create_order_for_insufficient_stock_last_item", "get_product_after_confirm_order_insufficient_stock_last_item"},
		"denied":   {"confirm_order_without_token", "confirm_order_with_bad_token", "fetch_order_after_confirm_order_denied"},
		"items":    {"create_order_1_lines", "confirm_order_1_lines", "create_order_3_lines", "confirm_order_3_lines"},
	}
	for tag, ids := range groups {
		planStep(t, p, "create_product_for_"+tag)
		for _, id := range ids {
			st := planStep(t, p, id)
			for _, ref := range st.References() {
				src := strings.SplitN(strings.TrimPrefix(ref, "steps."), ".", 2)[0]
				for _, fixture := range shared {
					if src == fixture {
						t.Fatalf("%s (group %s) reads the main path's %s, so a defect another probe leaves there fails it too:\n%s", id, tag, fixture, text)
					}
				}
			}
		}
	}
	if got := bodyAt(t, planStep(t, p, "create_product_for_shortage"), "sku"); got == bodyAt(t, planStep(t, p, "create_product"), "sku") {
		t.Fatalf("the group's product repeats the unique sku %s:\n%s", got, text)
	}
	planStep(t, p, "add_stock_for_shortage")
	if got := bodyAt(t, planStep(t, p, "confirm_order"), "id_order"); got != "${create_order.order.id_order}" {
		t.Fatalf("the main path keeps its fixtures, got %s", got)
	}
	if got := bodyAt(t, planStep(t, p, "confirm_order_as_clerk"), "id_order"); got != "${create_order_for_clerk.order.id_order}" {
		t.Fatalf("role parity keeps the fixtures it already owns, got %s", got)
	}
	if notes := strings.Join(p.Notes, "\n"); !strings.Contains(notes, "fixtures of their own") {
		t.Fatalf("the plan says the probe groups own their fixtures: %s", notes)
	}
}
