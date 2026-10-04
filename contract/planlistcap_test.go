package contract_test

import (
	"strings"
	"testing"
)

func TestPlanListsTwelveFixturesSoAResultCapShows(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "ListProducts")
	probe := planStep(t, p, "list_products_12_products")
	for _, id := range []string{"create_product", "create_product_4", "create_product_5", "create_product_12"} {
		if !strings.Contains(text, "id_product: ${"+id+".product.id_product}\n") {
			t.Fatalf("the probe asserts %s is listed:\n%s", id, text)
		}
	}
	wantExists(t, probe, "products.11", true)
	wantExists(t, probe, "products.12", false)
	if _, ok := p.Chain.Step("create_product_13"); ok {
		t.Fatalf("the probe plants 12 fixtures, reusing the 4 the plan has:\n%s", text)
	}
	if sku := bodyAt(t, planStep(t, p, "create_product_12"), "sku"); !strings.HasPrefix(sku, "sku-${vars.tag}-") {
		t.Fatalf("each extra fixture stays under the listed prefix, got %s", sku)
	}
	if !strings.Contains(notes, "states no limit") {
		t.Fatalf("a note says why the probe is there:\n%s", notes)
	}
}

func TestPlanListsOneMoreThanAStatedLimit(t *testing.T) {
	p := editedPlan(t, func(_, body string) string {
		return strings.Replace(body, "sorted by sku ascending;", "sorted by sku ascending, at most 6;", 1)
	}, "ListProducts")
	probe := planStep(t, p, "list_products_7_products")
	wantExists(t, probe, "products.5", true)
	wantExists(t, probe, "products.6", false)
	if _, ok := p.Chain.Step("create_product_8"); ok {
		t.Fatal("a stated limit of 6 needs 7 fixtures, no more")
	}
}
