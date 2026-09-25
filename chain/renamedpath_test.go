package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestLintNamesTheSiblingsOfAMissingLeafInCaseItWasRenamed(t *testing.T) {
	issues := expectPathIssues(t, []chain.Expectation{
		{Path: "error.kode", Equals: "OK"},
		{Path: "no_such.deep", Equals: "x"},
	})
	if len(issues) != 2 {
		t.Fatalf("want both bogus paths reported, got %v", issues)
	}
	if !strings.Contains(issues[0].Message, "if it was renamed in the proto, assert the new name: error declares code") {
		t.Fatalf("a missing leaf under a known parent names that parent's fields, in case the proto renamed it: %s", issues[0].Message)
	}
	if strings.Contains(issues[1].Message, "renamed") {
		t.Fatalf("a path whose parent does not exist gets no rename hint: %s", issues[1].Message)
	}
}
