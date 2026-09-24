package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestARespeltReferenceOrExpectationPathIsNoChainChange(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create_customer", Call: "S/CreateCustomer", Status: runner.StatusPassed, Response: []byte(`{}`)},
		{ID: "create_order", Call: "S/CreateOrder", Status: runner.StatusPassed, Response: []byte(`{}`),
			BodyRefs: map[string]string{"id_customer": "${create_customer.customer.id_customer}"},
			Expect:   []chain.ExpectResult{{Path: "order.total_minor", Rule: "equals", Want: 750, Passed: true}}},
	}}
	now := &chain.Chain{Name: "c", Steps: []*chain.Step{
		{ID: "create_customer", Call: "S/CreateCustomer"},
		{ID: "create_order", Call: "S/CreateOrder",
			Body:   map[string]any{"id_customer": "${create_customer.customer.idCustomer}"},
			Expect: []chain.Expectation{{Path: "order.totalMinor", Equals: 750}}},
	}}
	if changes := diff.ChainChanges(spot, now); len(changes) != 0 {
		t.Fatalf("idCustomer is the JSON name of id_customer and totalMinor of total_minor: no chain change, got %+v", changes)
	}
	now.Steps[1].Body["id_customer"] = "${create_customer.customer.email}"
	if changes := diff.ChainChanges(spot, now); len(changes) != 1 || !strings.HasPrefix(changes[0].Path, diff.BodyPathPrefix) {
		t.Fatalf("a reference to another field is still a chain change: %+v", changes)
	}
}

func TestAnUnorderedPathRespeltInCaseIsNotAnAddition(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "list", Call: "S/List", Status: runner.StatusPassed, Response: []byte(`{}`), Unordered: []string{"line_items"}},
	}}
	rec := runOf("run", &runner.StepRecord{ID: "list", Call: "S/List", Status: runner.StatusPassed, Response: []byte(`{}`), Unordered: []string{"lineItems"}})
	if added := diff.UnorderedAdded(spot, rec); len(added) != 0 {
		t.Fatalf("lineItems is line_items respelt, not an added unordered path: %v", added)
	}
}
