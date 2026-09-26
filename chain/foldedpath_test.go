package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func lintFetchExpect(t *testing.T, e chain.Expectation) []chain.Issue {
	t.Helper()
	c := &chain.Chain{Name: "probe", Steps: []*chain.Step{{
		ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "thing-1"},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}, e},
	}}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return chain.Lint(c, catalogtest.New())
}

func TestLintResolvesAFieldPathWithTheSameSeparatorFoldingAsRun(t *testing.T) {
	folded := false
	absent := false
	for _, e := range []chain.Expectation{{Path: "createdat", NotEmpty: true}, {Path: "CreatedAt", Exists: &absent}} {
		for _, i := range lintFetchExpect(t, e) {
			if i.Kind == chain.KindUnreachable || i.Kind == chain.KindUnfailable {
				t.Fatalf("run resolves %q to created_at, so lint must too: %s", e.Path, i.Message)
			}
		}
	}
	for _, i := range lintFetchExpect(t, chain.Expectation{Path: "no_such_field", NotEmpty: true}) {
		if i.Kind == chain.KindUnreachable {
			folded = true
		}
	}
	if !folded {
		t.Fatal("a path no folding resolves is still reported")
	}
}
