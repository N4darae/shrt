package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestACreateWhoseContractSaysZeroStockAssertsItAndTheReadAfter(t *testing.T) {
	p, text := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.catalog.v1.ProductService/CreateProduct"]; c != nil {
			c.Summary = "Add a product with zero stock on hand; ADMIN only."
		}
	}, "CreateProduct")
	wantExpect(t, planStep(t, p, "create_product"), "product.qty_on_hand", 0)
	wantExpect(t, planStep(t, p, "get_product_after_create_product"), "product.qty_on_hand", 0)
	if !strings.Contains(strings.Join(p.Notes, "\n"), `starts with none ("zero stock")`) {
		t.Fatalf("the plan says where the zero came from:\n%s", text)
	}
}

func TestAWriteWhoseContractSaysItDoesNotTouchStockIsFollowedByReadsOfTheLevels(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateOrder")
	for _, id := range []string{"get_product_after_create_order", "get_product_2_after_create_order"} {
		read := planStep(t, p, id)
		var level any
		for _, e := range read.Expect {
			if e.Path == "product.qty_on_hand" {
				level = e.Equals
			}
		}
		if level == nil {
			t.Fatalf("%s asserts the stock level create_order left alone:\n%s", id, text)
		}
	}
	add := planStep(t, p, "add_stock")
	wantExpect(t, planStep(t, p, "get_product_after_create_order"), "product.qty_on_hand", bodyAt(t, add, "qty"))
	if !strings.Contains(notes, `leaves what the plan tracks alone ("does not touch stock")`) {
		t.Fatalf("the plan says why it reads the stock after create_order:\n%s", notes)
	}
}
