package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestChainLintStrictHelpAndReadmeNameEveryWarningStrictFails(t *testing.T) {
	promotedWarnings := []string{
		chain.KindUnfailable, chain.KindAssertsNone, chain.KindInertAllowFail,
		chain.KindExportOverwritten, chain.KindArithmetic, chain.KindEnvelopeOnly,
	}
	for _, kind := range promotedWarnings {
		if !chain.IsAssertionQualityIssue(chain.Issue{Kind: kind}) {
			t.Fatalf("%s is no longer promoted by -strict; update this list and the docs", kind)
		}
	}
	help := strings.Join(strings.Fields(helpOf(t, "chain", "lint")), " ")
	readme := strings.Join(strings.Fields(string(mustRead(t, "../../README.md"))), " ")
	_, row, _ := strings.Cut(readme, "| `shrt chain lint [<c>]` |")
	row, _, _ = strings.Cut(row, "| `shrt chain ls` |")
	for _, kind := range promotedWarnings {
		if !strings.Contains(help, kind) {
			t.Errorf("chain lint -h -strict must name %s:\n%s", kind, help)
		}
		if !strings.Contains(row, kind) {
			t.Errorf("README's chain lint row must name %s among what -strict fails", kind)
		}
	}
}
