package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestNoFixtureOfOneTagEqualsAFixtureOfAnother(t *testing.T) {
	for _, target := range []string{"ConfirmOrder", "ListProducts", "ListOrders"} {
		p, text := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
			if c := rpcs["shop.catalog.v1.ProductService/CreateProduct"]; c != nil {
				c.Fields["sku"].Value = "sku-${vars.tag}-"
			}
		}, target)
		values := map[string]bool{}
		for _, st := range p.Chain.Steps {
			if !strings.HasSuffix(st.Call, "/CreateProduct") && !strings.HasSuffix(st.Call, "/CreateCustomer") {
				continue
			}
			for _, key := range []string{"sku", "email"} {
				if v, ok := st.Body[key].(string); ok && strings.Contains(v, "${vars.tag}") {
					values[v] = true
				}
			}
		}
		for a := range values {
			for b := range values {
				for _, tags := range [][2]string{{"x", "x-2"}, {"x", "x-3"}, {"x-2", "x"}} {
					one := strings.ReplaceAll(a, "${vars.tag}", tags[0])
					other := strings.ReplaceAll(b, "${vars.tag}", tags[1])
					if one == other {
						t.Fatalf("%s: %s with tag %s equals %s with tag %s (%s):\n%s", target, a, tags[0], b, tags[1], one, text)
					}
				}
			}
		}
	}
}

func TestTheSecondProductKeepsTheListPrefixWithItsOrdinalAfterTheTerminator(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "ListProducts")
	prefix := bodyAt(t, planStep(t, p, "list_products"), "sku_prefix")
	for _, id := range []string{"create_product_2", "create_product_3"} {
		sku := bodyAt(t, planStep(t, p, id), "sku")
		if !strings.HasPrefix(sku, prefix) || strings.HasSuffix(sku, "-") {
			t.Fatalf("%s's sku %s starts with the prefix %s and does not end with its terminator:\n%s", id, sku, prefix, text)
		}
	}
}

func TestASecondFixtureOfAValueEndingInTheVarIsNoted(t *testing.T) {
	_, _, notes := shopDemoPlan(t, "ConfirmOrder")
	if !strings.Contains(notes, `sku "sku-${vars.tag}" ends in the var`) {
		t.Fatalf("a unique value ending in the var cannot keep another tag's second fixture apart, and the plan says so:\n%s", notes)
	}
}
