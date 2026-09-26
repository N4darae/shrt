package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
)

func TestAChainLevelUnorderedAdditionIsNamedOnce(t *testing.T) {
	rec := listRun(listRunReversed, nil)
	for _, st := range rec.Steps {
		st.Unordered = []string{"results"}
	}
	rec.Steps[2].Unordered = []string{"results", "products"}
	added := diff.UnorderedAdded(listSpot(), rec)
	if len(added) != 2 {
		t.Fatalf("a path added on every step is one chain-level line, and a step's own addition one more: %q", added)
	}
	if !strings.Contains(added[0], "`unordered: [results]` at chain level") || !strings.Contains(added[1], "`unordered: [products]` on step list") {
		t.Fatalf("want the chain-level addition once and the step's own addition by step: %q", added)
	}
}
