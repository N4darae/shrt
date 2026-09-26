package store_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/store"
)

func TestAFieldChangedInASameLengthListIsNamedWithItsValues(t *testing.T) {
	rec := passingRun("run-2")
	p := &store.Proposal{Chain: rec.Chain, RunID: rec.RunID, ComparedTo: "run-1",
		Unstable: []string{"list_customer_orders orders.0.total_minor: 750 -> 1"}}
	text := store.ProposalSummary(p, rec)
	if strings.Contains(text, "`volatile: [orders]`") || strings.Contains(text, "items change from run to run") {
		t.Fatalf("one item in both runs with one field changed is not a list that changes from run to run:\n%s", text)
	}
	for _, want := range []string{"`orders.0.total_minor` 750 -> 1", "may be a real change", "`volatile: [orders.*.total_minor]`"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the warning lacks %q:\n%s", want, text)
		}
	}
}

func TestAFieldNewInListItemsIsNotWholeListVolatile(t *testing.T) {
	rec := passingRun("run-2")
	p := &store.Proposal{Chain: rec.Chain, RunID: rec.RunID, ComparedTo: "run-1",
		Unstable: []string{"list_orders orders.0.note: absent -> gift wrap", "list_orders orders.1.note: absent -> gift wrap"}}
	text := store.ProposalSummary(p, rec)
	if strings.Contains(text, "`volatile: [orders]`") || strings.Contains(text, "items change from run to run") {
		t.Fatalf("a field new in every item of a same-length list is a field change, not a list that changes every run:\n%s", text)
	}
	if !strings.Contains(text, "`volatile: [orders.*.note]`") {
		t.Fatalf("the advice should name the field pattern:\n%s", text)
	}
}

func TestAListThatGrewKeepsTheWholeListAdvice(t *testing.T) {
	rec := passingRun("run-2")
	p := &store.Proposal{Chain: rec.Chain, RunID: rec.RunID, ComparedTo: "run-1",
		Unstable: []string{"list_all products: 1 item(s) -> 2 item(s)", "list_all products.1: absent -> map[name:b]"}}
	text := store.ProposalSummary(p, rec)
	if !strings.Contains(text, "`volatile: [products]`") || !strings.Contains(text, "1 item(s) -> 2 item(s)") {
		t.Fatalf("a list that grew keeps the whole-list advice and says how it grew:\n%s", text)
	}
}
