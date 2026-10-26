package contract_test

import "testing"

func TestPlanForAnIncreasingWriteReadsTheRecordBeforeAndAfterIt(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "AddStock")
	before := stepIndex(p.Chain, "get_product_before_add_stock")
	act := stepIndex(p.Chain, "add_stock")
	after := stepIndex(p.Chain, "get_product_after_add_stock")
	if before < 0 || after < 0 || before+1 != act || act+1 != after {
		t.Fatalf("add_stock sits between a read of its product before it and one after it:\n%s", text)
	}
	if got := bodyAt(t, p.Chain.Steps[before], "id_product"); got != "${create_product.product.id_product}" {
		t.Fatalf("the read before add_stock reads the product it stocks, got %s:\n%s", got, text)
	}
	wantExpect(t, p.Chain.Steps[after], "product.qty_on_hand", 5)
}
