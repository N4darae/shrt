package contract_test

import (
	"strings"
	"testing"
)

func TestPlanForAUniquenessRefusalSendsTheUniqueValueWithEveryOtherFieldChanged(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateProduct")
	base := planStep(t, p, "create_product")
	dup := planStep(t, p, "create_product_same_sku_other_fields")
	if got := bodyAt(t, dup, "sku"); got != "${steps.create_product.request.sku}" {
		t.Fatalf("the unique field is the one value kept equal, got sku %s:\n%s", got, text)
	}
	for _, field := range []string{"name", "price_minor"} {
		if bodyAt(t, dup, field) == bodyAt(t, base, field) {
			t.Fatalf("%s must differ from the original so only the sku matches: a backend that compares the whole "+
				"record passes an exact copy:\n%s", field, text)
		}
	}
	wantExpect(t, dup, "status.details.0.reason", "SkuTaken")
	wantExpect(t, dup, "status.details.0.app_code", 1201)
	planStep(t, p, "create_product_same_sku")
	if !strings.Contains(notes, "create_product_same_sku_other_fields") {
		t.Fatalf("the plan says why it changes the other fields: %s", notes)
	}
}
