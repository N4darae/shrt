package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func TestErrorCountCountsBothKindsOfIssue(t *testing.T) {
	overlay := []contract.Issue{
		{Severity: contract.SeverityError}, {Severity: contract.SeverityWarn}, {Severity: contract.SeverityError},
	}
	if n := contract.ErrorCount(overlay); n != 2 {
		t.Errorf("overlay issues: ErrorCount = %d, want 2", n)
	}
	chained := []chain.Issue{
		{Severity: chain.SeverityWarn}, {Severity: chain.SeverityError},
	}
	if n := contract.ErrorCount(chained); n != 1 {
		t.Errorf("chain issues: ErrorCount = %d, want 1", n)
	}
	if n := contract.ErrorCount([]contract.Issue{}); n != 0 {
		t.Errorf("empty: ErrorCount = %d, want 0", n)
	}
}
