package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestStrictPromotionIsKeyedOnKindNotOnMessageText(t *testing.T) {
	reworded := chain.Issue{
		Severity: chain.SeverityWarn,
		Kind:     chain.KindUnfailable,
		Message:  "a maintainer reworded this sentence and it no longer contains the old marker",
	}
	got := chain.Promote([]chain.Issue{reworded}, chain.IsAssertionQualityIssue)
	if got[0].Severity != chain.SeverityError {
		t.Fatal("promotion missed an issue whose Kind says it is an assertion-quality problem. Keying on " +
			"a substring of the human message means any copy-edit silently drops a rule from the gate, " +
			"with nothing red to say so")
	}

	prose := chain.Issue{
		Severity: chain.SeverityWarn,
		Message:  "this message happens to contain the words cannot fail but carries no Kind",
	}
	if chain.Promote([]chain.Issue{prose}, chain.IsAssertionQualityIssue)[0].Severity != chain.SeverityWarn {
		t.Error("promotion fired on message text alone, so an unrelated warning that quotes the phrase " +
			"becomes a build failure")
	}
}

func TestEveryAssertionQualityKindIsPromoted(t *testing.T) {
	kinds := []string{
		chain.KindUnfailable, chain.KindAssertsNone, chain.KindUnreachable,
		chain.KindDeadRef, chain.KindBadExport,
	}
	for _, k := range kinds {
		i := chain.Issue{Severity: chain.SeverityWarn, Kind: k, Message: "x"}
		if !chain.IsAssertionQualityIssue(i) {
			t.Errorf("kind %q describes a chain that cannot run correctly and is not promoted by -strict, "+
				"so it ships green and dies at run time after earlier steps have already hit the backend", k)
		}
	}
}
