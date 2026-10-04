package contract

import "testing"

func TestAContractSayingTheWriteGivesItBackInAnyTenseRestores(t *testing.T) {
	for _, text := range []string{
		"Move an invoice to VOID, returning its credit if it was POSTED.",
		"Voiding a POSTED invoice returns its credit.",
		"Voids the invoice; a POSTED invoice has its credit restored.",
		"Voids the invoice, restoring the credit of a POSTED one.",
		"Voids the invoice, giving the credit back when it was POSTED.",
		"Voids the invoice; a POSTED invoice is refunded.",
	} {
		if namingClause([]string{text}, "POSTED", restoreWord()) == "" {
			t.Errorf("%q says the write gives back what POSTED took", text)
		}
	}
	for _, text := range []string{
		"Move an invoice to VOID; a POSTED invoice keeps its credit.",
		"Returns the invoice as VOID.; POSTED invoices are archived",
	} {
		if namingClause([]string{text}, "POSTED", restoreWord()) != "" {
			t.Errorf("%q says nothing is given back from POSTED", text)
		}
	}
}
