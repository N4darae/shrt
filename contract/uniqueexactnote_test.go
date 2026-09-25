package contract_test

import (
	"strings"
	"testing"
)

func TestPlanDoesNotSuggestACaseVariantWhenTheContractSaysTheComparisonIsExact(t *testing.T) {
	for _, unique := range []string{"{case: exact}", "{case: exact, trim: false}"} {
		_, notes := planYAML(t, uniqueOverlay(unique))
		if strings.Contains(notes, "set unique: {case: ignore}") {
			t.Fatalf("unique: %s already answers the case question, so the plan does not ask it:\n%s", unique, notes)
		}
	}
	_, notes := planYAML(t, customerOverlay("must be unused", "another customer already has exactly this email, case-sensitive"))
	if strings.Contains(notes, "set unique: {case: ignore}") {
		t.Fatalf("a when: saying case-sensitive answers the case question too:\n%s", notes)
	}
	_, notes = planYAML(t, customerOverlay("must be unused", "another customer already has this email"))
	if !strings.Contains(notes, "set unique: {case: ignore}") {
		t.Fatalf("a contract silent on case still gets the suggestion:\n%s", notes)
	}
}
