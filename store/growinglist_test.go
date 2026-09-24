package store_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/store"
)

func TestAGrowingListIsOneLineWithACountAndTheFix(t *testing.T) {
	rec := passingRun("run-2")
	unstable := []string{"list_all products", "create product.sku"}
	for i := 30; i < 400; i++ {
		unstable = append(unstable, fmt.Sprintf("list_all products.%d.name", i), fmt.Sprintf("list_all products.%d.sku", i))
	}
	p := &store.Proposal{Chain: rec.Chain, RunID: rec.RunID, ComparedTo: "run-1", Unstable: unstable}
	text := store.ProposalSummary(p, rec)
	if n := strings.Count(text, "\n"); n > 40 {
		t.Fatalf("a list of 742 changed fields must not print a line each (%d lines):\n%s", n, text)
	}
	if !strings.Contains(text, "`list_all products`: 741 field(s)") {
		t.Fatalf("the list must be named once with its count:\n%s", text)
	}
	if !strings.Contains(text, "`volatile: [products]` on step `list_all`") || !strings.Contains(text, "`unordered: [products]`") {
		t.Fatalf("the warning must suggest the fix on the step:\n%s", text)
	}
	if !strings.Contains(text, "`create product.sku`") {
		t.Fatalf("a field outside a list is still named alone:\n%s", text)
	}
}
