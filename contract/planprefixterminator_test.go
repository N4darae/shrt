package contract_test

import (
	"strings"
	"testing"
)

func TestPlanForAPrefixListEndsThePrefixWithTheTerminatorItsFixturesCarry(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "ListProducts")
	prefix := bodyAt(t, planStep(t, p, "list_products"), "sku_prefix")
	if !strings.HasSuffix(prefix, "}-") {
		t.Fatalf("a prefix ending in ${vars.tag} also lists another run's fixtures when one tag is a prefix of another (cp-1, cp-10): want a terminator after the var, got %s\n%s", prefix, text)
	}
	for _, id := range []string{"create_product", "create_product_2", "create_product_3"} {
		if sku := bodyAt(t, planStep(t, p, id), "sku"); !strings.HasPrefix(sku, prefix) {
			t.Fatalf("%s's sku %s must still start with the prefix %s", id, sku, prefix)
		}
	}
	if !strings.Contains(notes, "terminator") {
		t.Fatalf("the plan says why the prefix ends where it does: %s", notes)
	}
}
