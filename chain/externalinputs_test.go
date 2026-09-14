package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func inputChain(t *testing.T, vars map[string]any, body map[string]any) *chain.Chain {
	t.Helper()
	c := &chain.Chain{
		Name: "inputs",
		Vars: vars,
		Steps: []*chain.Step{{
			ID:     "create",
			Call:   "ThingService/Create",
			Body:   body,
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
		}},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestExternalInputsNamesVarsTheChainDoesNotDeclare(t *testing.T) {
	c := inputChain(t, map[string]any{"declared": "x"}, map[string]any{
		"name":            "${vars.declared}",
		"kind":            "${vars.undeclared_one}",
		"idempotency_key": "${vars.undeclared_two}",
	})

	vars, env := chain.ExternalInputs(c)
	if len(env) != 0 {
		t.Fatalf("env = %v, want none", env)
	}
	if strings.Join(vars, ",") != "undeclared_one,undeclared_two" {
		t.Fatalf("vars = %v, want the two undeclared ones in sorted order", vars)
	}
}

func TestExternalInputsNamesEveryEnvVarTheChainReads(t *testing.T) {
	c := inputChain(t, nil, map[string]any{
		"name":            "${env.B_NAME}",
		"kind":            "${env.A_KIND}",
		"idempotency_key": "${uuid}",
	})

	_, env := chain.ExternalInputs(c)
	if strings.Join(env, ",") != "A_KIND,B_NAME" {
		t.Fatalf("env = %v, want [A_KIND B_NAME]", env)
	}
}

func TestLintWarnsAboutUndeclaredVarsWithoutFailingTheChain(t *testing.T) {
	c := inputChain(t, nil, map[string]any{
		"name":            "${vars.business_date}",
		"kind":            "KIND_A",
		"idempotency_key": "${uuid}",
	})

	issues := chain.Lint(c, catalogtest.New())
	warned := false
	for _, i := range issues {
		if i.Severity == chain.SeverityError {
			t.Fatalf("an undeclared var must not be an error — it can legitimately arrive via -var: %v", i)
		}
		if strings.Contains(i.Message, "business_date") && strings.Contains(i.Message, "-var business_date=") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("lint said nothing about ${vars.business_date}, so a chain that cannot run without it "+
			"reports ok and the requirement is discovered one variable at a time at run time; issues = %v", issues)
	}
}

func TestLintSaysNothingWhenEveryVarIsDeclared(t *testing.T) {
	c := inputChain(t, map[string]any{"business_date": "2026-01-01"}, map[string]any{
		"name":            "${vars.business_date}",
		"kind":            "KIND_A",
		"idempotency_key": "${uuid}",
	})

	for _, i := range chain.Lint(c, catalogtest.New()) {
		if strings.Contains(i.Message, "business_date") {
			t.Fatalf("a declared var was reported as an external input: %v", i)
		}
	}
}

func TestLintWarnsAboutAStepThatAssertsNothing(t *testing.T) {
	c := inputChain(t, nil, map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"})
	c.Steps[0].Expect = nil

	warned := false
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if i.Severity == chain.SeverityError {
			t.Fatalf("a step with no assertions must not be an error — a deliberate setup step is legitimate: %v", i)
		}
		if strings.Contains(i.Message, "asserts nothing at all") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("nothing was said about a step asserting nothing. Rule 4 says a step must not assert only " +
			"that the server did not crash; a step asserting NOTHING is weaker still, and no gate saw it")
	}
}

func TestLintSaysNothingAboutAStepThatDoesAssert(t *testing.T) {
	c := inputChain(t, nil, map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"})
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if strings.Contains(i.Message, "asserts nothing at all") {
			t.Fatalf("a step with an expectation was reported as asserting nothing: %v", i)
		}
	}
}

func envIssues(t *testing.T, opts chain.LintOptions) []chain.Issue {
	t.Helper()
	c := inputChain(t, nil, map[string]any{"name": "${env.WIDGET_OWNER}", "kind": "KIND_A"})
	out := []chain.Issue{}
	for _, i := range chain.LintWith(c, catalogtest.New(), opts) {
		if strings.Contains(i.Message, "WIDGET_OWNER") {
			out = append(out, i)
		}
	}
	return out
}

func TestLintSaysNothingAboutAnEnvVarThatIsExported(t *testing.T) {
	exported := chain.LintOptions{Env: func(k string) (string, bool) { return "owner", k == "WIDGET_OWNER" }}
	if issues := envIssues(t, exported); len(issues) != 0 {
		t.Fatalf("WIDGET_OWNER is exported, so a warning that it must be exported is false and costs the "+
			"chain its ok line: %+v", issues)
	}
}

func TestLintWarnsAboutAnEnvVarThatIsNotExported(t *testing.T) {
	unset := chain.LintOptions{Env: func(string) (string, bool) { return "", false }}
	issues := envIssues(t, unset)
	if len(issues) != 1 || issues[0].IsError() {
		t.Fatalf("an unset env var is a warning about this shell, not a defect in the chain: %+v", issues)
	}
	if !strings.Contains(issues[0].Message, "not exported") {
		t.Errorf("the warning must say the variable is unset here: %q", issues[0].Message)
	}
}

func TestLintWithoutAnEnvironmentDoesNotClaimAVariableIsUnset(t *testing.T) {
	for _, i := range envIssues(t, chain.LintOptions{}) {
		if strings.Contains(i.Message, "not exported") || strings.Contains(i.Message, "exported before") {
			t.Fatalf("lint was given no environment, so it cannot know what is exported: %q", i.Message)
		}
	}
}
