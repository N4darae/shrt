package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestARefusalWhoseOnlyReaderAnswersTextIsANoteNotAGap(t *testing.T) {
	p, _ := shopDemoMutated(t, contract.PlanOptions{}, func(map[string]*contract.RPCContract) {}, "CreateOrder")
	found := false
	for _, n := range p.Notes {
		if strings.Contains(n, "GetCustomer reads what it touches but answers only text") {
			found = true
			if _, gap := contract.GapOf(n); gap {
				t.Fatalf("a refusal no contract edit can guard is a note, not a gap: %s", n)
			}
		}
	}
	for _, g := range p.GapNotes() {
		if strings.Contains(g, "GetCustomer") {
			t.Fatalf("no gap about the text-only reader: %s", g)
		}
	}
	if !found {
		t.Fatalf("the plan says why no read guards the refusal: %v", p.Notes)
	}
}
