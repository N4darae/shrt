package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func refSyntaxIssues(t *testing.T, name string) []chain.Issue {
	t.Helper()
	c := &chain.Chain{Name: "syntax", Steps: []*chain.Step{{
		ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": name, "kind": "KIND_A"},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	}}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	out := []chain.Issue{}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if i.Kind == chain.KindRefSyntax {
			out = append(out, i)
		}
	}
	return out
}

func TestLintWarnsOnReferenceSyntaxThatResolvesToSomethingElse(t *testing.T) {
	cases := map[string]string{
		"a-${ uuid }":   "spaces",
		"a=${now+1.5}":  "whole",
		"i=${uuid":      "literal",
		"x-${uuid.id}":  "takes no path",
		"${today-1.25}": "whole",
	}
	for name, want := range cases {
		got := refSyntaxIssues(t, name)
		if len(got) != 1 || got[0].Severity != chain.SeverityWarn || !strings.Contains(got[0].Message, want) {
			t.Errorf("%q is accepted but does not do what it reads as; want one warning mentioning %q, got %+v", name, want, got)
		}
	}
}

func TestLintAcceptsWellFormedReferences(t *testing.T) {
	for _, name := range []string{"a-${uuid}", "${now+3600}", "$${uuid}", "plain $ text", "{braces}"} {
		if got := refSyntaxIssues(t, name); len(got) != 0 {
			t.Errorf("%q is well formed, got %+v", name, got)
		}
	}
}
