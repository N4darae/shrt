package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func listWidgets(stepUnordered, chainUnordered []string) *chain.Chain {
	c := &chain.Chain{Name: "unord-lint", Unordered: chainUnordered, Steps: []*chain.Step{{
		ID: "list", Call: "WidgetService/ListWidgets", Body: map[string]any{"id": "x"},
		Unordered: stepUnordered,
		Expect:    []chain.Expectation{{Path: "result.code", Equals: "OK"}},
	}}}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func unorderedErrors(c *chain.Chain) []string {
	out := []string{}
	for _, i := range chain.Lint(c, catalogtest.Listing()) {
		if i.IsError() && strings.Contains(i.Message, "unordered") {
			out = append(out, i.Message)
		}
	}
	return out
}

func TestUnorderedPathsMustNameARepeatedResponseField(t *testing.T) {
	for _, bad := range []string{"widgts", "widgets.0", "**", "widgets.*", "result", "widgets.id"} {
		if got := unorderedErrors(listWidgets([]string{bad}, nil)); len(got) != 1 || !strings.Contains(got[0], bad) {
			t.Errorf("step unordered %q must be a lint error naming it, got %v", bad, got)
		}
		if got := unorderedErrors(listWidgets(nil, []string{bad})); len(got) != 1 || !strings.Contains(got[0], bad) {
			t.Errorf("chain unordered %q must be a lint error naming it, got %v", bad, got)
		}
	}
	if got := unorderedErrors(listWidgets([]string{"widgets"}, []string{"widgets"})); len(got) != 0 {
		t.Errorf("widgets is a repeated field of the response: %v", got)
	}
}
