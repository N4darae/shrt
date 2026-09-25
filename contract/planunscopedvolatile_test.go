package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAListStepNotScopedToTheRunDeclaresItsListVolatileAndKeepsIncludes(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "ListProducts")
	probe := planStep(t, p, "list_products_empty_sku_prefix")
	if len(probe.Volatile) != 1 || probe.Volatile[0] != "products" {
		t.Fatalf("an unfiltered list grows with every run, so its list is volatile, got %v:\n%s", probe.Volatile, text)
	}
	includes := 0
	for _, e := range probe.Expect {
		if e.Path == "products" && e.Includes != nil {
			includes++
		}
	}
	if includes == 0 {
		t.Fatalf("membership of the run's fixtures is still asserted with includes:\n%s", text)
	}
	if scoped := planStep(t, p, "list_products"); len(scoped.Volatile) != 0 {
		t.Fatalf("a list scoped to the run's prefix is compared in full, got volatile %v:\n%s", scoped.Volatile, text)
	}
	if !strings.Contains(notes, "list_products_empty_sku_prefix lists everything the backend holds") {
		t.Fatalf("the plan says why the list is volatile:\n%s", notes)
	}
	for _, st := range p.Chain.Steps {
		if st.ID != probe.ID && chain.IsReadOnlyCall(st.Call) && len(st.Volatile) > 0 && !strings.Contains(st.ID, "empty") {
			t.Fatalf("step %s is scoped to the run and needs no volatile list, got %v", st.ID, st.Volatile)
		}
	}
}
