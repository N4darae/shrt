package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func unscopedIssues(prefix string, expect ...chain.Expectation) []chain.Issue {
	s := &chain.Step{
		ID: "list_products", Call: "shop.catalog.v1.ProductService/ListProducts", SkipAuth: true,
		Body:   map[string]any{"sku_prefix": prefix},
		Expect: append([]chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}, expect...),
	}
	c := &chain.Chain{Name: "t", Steps: []*chain.Step{s}}
	_ = c.Normalize()
	out := []chain.Issue{}
	for _, i := range chain.Lint(c, catalogtest.Shop()) {
		if i.Kind == chain.KindUnscopedCount {
			out = append(out, i)
		}
	}
	return out
}

func TestAnExactCountOnAListNothingScopesIsWarned(t *testing.T) {
	no, yes := false, true
	got := unscopedIssues("", chain.Expectation{Path: "products.3", Exists: &no})
	if len(got) != 1 || got[0].Severity != chain.SeverityWarn {
		t.Fatalf("want one unscoped-count warning, got %v", got)
	}
	for _, want := range []string{"products.3 exists: false", "second run", "products.2 exists: true"} {
		if !strings.Contains(got[0].Message, want) {
			t.Errorf("the warning must say %q: %s", want, got[0].Message)
		}
	}
	if got := unscopedIssues("sku-${vars.tag}-", chain.Expectation{Path: "products.3", Exists: &no}); len(got) != 0 {
		t.Fatalf("a prefix built from a var scopes the list to this run: %v", got)
	}
	if got := unscopedIssues("", chain.Expectation{Path: "products.2", Exists: &yes}); len(got) != 0 {
		t.Fatalf("a lower bound holds however many items other runs left: %v", got)
	}
}
