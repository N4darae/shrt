package diff_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestARewiredReferenceIsAChainChangeForAnOlderSafeSpot(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create_customer", Call: "S/Create", Status: runner.StatusPassed, Request: []byte(`{"name":"a"}`), Response: []byte(`{"customer":{"id_customer":"cus-1"}}`)},
		{ID: "create_customer_2", Call: "S/Create", Status: runner.StatusPassed, Request: []byte(`{"name":"b"}`), Response: []byte(`{"customer":{"id_customer":"cus-2"}}`)},
		{ID: "create_order", Call: "S/Order", Status: runner.StatusPassed, Request: []byte(`{"id_customer":"cus-1","lines":[{"id_product":"cus-2"}]}`), Response: []byte(`{}`)},
	}}
	body := func(customer string) map[string]any {
		return map[string]any{"id_customer": customer, "lines": []any{map[string]any{"id_product": "${create_customer_2.customer.id_customer}"}}}
	}
	steps := func(customer string) []*chain.Step {
		return []*chain.Step{{ID: "create_customer", Call: "S/Create"}, {ID: "create_customer_2", Call: "S/Create"}, {ID: "create_order", Call: "S/Order", Body: body(customer)}}
	}
	same := diff.ChainChanges(spot, &chain.Chain{Steps: steps("${create_customer.customer.id_customer}")})
	if len(same) != 0 {
		t.Fatalf("the chain reads what the confirmed run read, no change: %+v", same)
	}
	got := diff.ChainChanges(spot, &chain.Chain{Steps: steps("${create_customer_2.customer.id_customer}")})
	if len(got) != 1 || got[0].Step != "create_order" || got[0].Path != "body.id_customer" ||
		got[0].Transition() != "${create_customer.customer.id_customer} -> ${create_customer_2.customer.id_customer}" {
		t.Fatalf("the rewired reference is a chain change naming both references: %+v", got)
	}
	rep := &diff.Report{RequestChanges: got}
	if !rep.OnlyChainChanged() {
		t.Error("a rewired reference is a chain change, not different input")
	}
}

func TestAStoredReferenceChangeIsAChainChange(t *testing.T) {
	spot := &store.SafeSpot{Steps: []*runner.StepRecord{
		{ID: "a", Call: "S/A"},
		{ID: "b", Call: "S/B", Request: []byte(`{"id":"x-1"}`), BodyRefs: map[string]string{"id": "${a.id}"}},
	}}
	now := &chain.Chain{Steps: []*chain.Step{{ID: "a", Call: "S/A"}, {ID: "b", Call: "S/B", Body: map[string]any{"id": "${a.other_id}"}}}}
	got := diff.ChainChanges(spot, now)
	if len(got) != 1 || got[0].Transition() != "${a.id} -> ${a.other_id}" {
		t.Fatalf("the stored reference differs from the chain's: %+v", got)
	}
}
