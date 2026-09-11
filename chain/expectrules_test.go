package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func lintChain(t *testing.T, c *chain.Chain) []chain.Issue {
	t.Helper()
	for _, st := range c.Steps {
		if len(st.Expect) == 0 {
			st.Expect = []chain.Expectation{{Path: "error.code", Equals: "OK"}}
		}
	}
	if err := c.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	return chain.Lint(c, catalogtest.New())
}

func TestLintRejectsTwoRulesOnOneExpectation(t *testing.T) {
	issues := lintOf(t, &chain.Step{ID: "fetch", Call: "ThingService/Fetch",
		Body: map[string]any{"id": "x"},
		Expect: []chain.Expectation{
			{Path: "name", Equals: "widget", NotEmpty: true},
		}})
	if len(issues) != 1 || issues[0].Severity != chain.SeverityError {
		t.Fatalf("two rules on one entry must be an error: Evaluate runs a fixed-precedence switch, so not_empty fires and the equals is discarded while the entry still reads as checked. got %v", issues)
	}
	if !strings.Contains(issues[0].Message, "not_empty") || !strings.Contains(issues[0].Message, "equals") {
		t.Errorf("the message must name both rules and which one wins, got %q", issues[0].Message)
	}
}

func TestLintRejectsAnExpectationWithNoRule(t *testing.T) {
	issues := lintOf(t, &chain.Step{ID: "fetch", Call: "ThingService/Fetch",
		Body:   map[string]any{"id": "x"},
		Expect: []chain.Expectation{{Path: "error.code"}}})
	if len(issues) != 1 {
		t.Fatalf("a path with no rule asserts nothing and must not lint clean, got %v", issues)
	}
}

func TestLintAcceptsTheSameTwoRulesSplitApart(t *testing.T) {
	issues := lintOf(t, &chain.Step{ID: "fetch", Call: "ThingService/Fetch",
		Body: map[string]any{"id": "x"},
		Expect: []chain.Expectation{
			{Path: "error.code", Equals: "OK"},
			{Path: "name", NotEmpty: true},
		}})
	if len(issues) != 0 {
		t.Fatalf("split entries each carry one rule and both fire, got %v", issues)
	}
}

func TestLintRejectsAReferenceInsideAVarValue(t *testing.T) {
	issues := lintChain(t, &chain.Chain{Name: "t",
		Vars: map[string]any{"shared_key": "${uuid}"},
		Steps: []*chain.Step{{ID: "fetch", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${vars.shared_key}"}}}})
	if len(issues) != 1 || issues[0].Severity != chain.SeverityError {
		t.Fatalf("a var value is handed back verbatim, never resolved, so ${uuid} there sends the literal text and every check still passes. got %v", issues)
	}
}

func TestLintAcceptsAPlainVarValue(t *testing.T) {
	issues := lintChain(t, &chain.Chain{Name: "t",
		Vars: map[string]any{"tag": "TPO", "qty": 40000},
		Steps: []*chain.Step{{ID: "fetch", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${vars.tag}"}}}})
	if len(issues) != 0 {
		t.Fatalf("want no issues, got %v", issues)
	}
}

func TestNotEqualFailsWhenThePathIsAbsent(t *testing.T) {
	e := chain.Expectation{Path: "rows.0.parked_out_receivable_qty", NotEqual: "0"}
	got := e.Evaluate(map[string]any{"rows": []any{}})
	if got.Passed {
		t.Fatalf("not_equal on an unresolved path must FAIL: an empty rows list made a money assertion read as checked while it measured nothing — observed in gate-a2-parking-counts-toward-credit run 20260909T100508Z. got %+v", got)
	}
	if !strings.Contains(got.Detail, "not present") {
		t.Errorf("the detail must say the path never resolved, got %q", got.Detail)
	}
}

func TestNotEqualStillPassesOnAResolvedDifferentValue(t *testing.T) {
	e := chain.Expectation{Path: "rows.0.qty", NotEqual: "0"}
	got := e.Evaluate(map[string]any{"rows": []any{map[string]any{"qty": "2000000"}}})
	if !got.Passed {
		t.Fatalf("a present value that differs must still pass, got %+v", got)
	}
}
