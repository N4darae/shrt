package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

var orderLifecycle = []string{"CreateProduct", "CreateProduct", "AddStock", "AddStockBatch", "CreateCustomer", "CreateOrder",
	"GetProduct", "ConfirmOrder", "GetProduct", "GetProduct", "FetchOrder", "CancelOrder", "ListOrders", "FetchOrder", "CancelOrder"}

func chainNewShopDemo(t *testing.T, rpcs ...string) (*chain.Chain, string, []string) {
	t.Helper()
	cat, lib := shopDemo(t)
	refs := make([]string, 0, len(rpcs))
	ids := make([]string, 0, len(rpcs))
	seen := map[string]bool{}
	for _, rpc := range rpcs {
		m, err := cat.Lookup(rpc)
		if err != nil {
			t.Fatal(err)
		}
		base := contract.For(m).StepID()
		id := base
		for i := 2; seen[id]; i++ {
			id = base + "_" + string(rune('0'+i))
		}
		seen[id] = true
		refs = append(refs, m.FullName)
		ids = append(ids, id)
	}
	raw, notes, err := contract.ScaffoldChain("flow", "", refs, ids, lib, cat)
	if err != nil {
		t.Fatal(err)
	}
	var c chain.Chain
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	return &c, string(raw), notes
}

func TestChainNewNamesARepeatedRPCAfterWhatItObserves(t *testing.T) {
	c, raw, notes := chainNewShopDemo(t, orderLifecycle...)
	want := []string{"create_product", "create_product_2", "add_stock", "add_stock_batch", "create_customer", "create_order",
		"get_product_after_create_order", "confirm_order", "get_product_after_confirm_order", "get_product_2_after_confirm_order",
		"fetch_order_after_confirm_order", "cancel_order", "list_orders", "fetch_order_after_cancel_order", "cancel_order_again"}
	got := []string{}
	for _, st := range c.Steps {
		got = append(got, st.ID)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("ids:\n got %v\nwant %v\n%s", got, want, raw)
	}
	reads := map[string]string{
		"get_product_after_confirm_order":   "${create_product.product.id_product}",
		"get_product_2_after_confirm_order": "${create_product_2.product.id_product}",
	}
	for id, ref := range reads {
		st, _ := c.Step(id)
		if st.Body["id_product"] != ref {
			t.Fatalf("%s must read %s, got %v:\n%s", id, ref, st.Body["id_product"], raw)
		}
	}
	for _, st := range c.Steps {
		for _, ref := range st.References() {
			src, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(ref), "steps."), ".")
			if _, ok := c.Step(src); !ok && src != "vars" && src != "uuid" && src != "nowunix" {
				t.Fatalf("step %s references %s, which is no step:\n%s", st.ID, ref, raw)
			}
		}
	}
	joined := strings.Join(notes, "\n")
	if strings.Contains(joined, "again after") {
		t.Fatalf("the id says what a read observes, so no note repeats it:\n%s", joined)
	}
	for _, n := range notes {
		if strings.Contains(n, "get_product_2:") || strings.Contains(n, "get_product_3") || strings.Contains(n, "fetch_order_2") {
			t.Fatalf("notes must name the steps by the ids written:\n%s", joined)
		}
	}
}
