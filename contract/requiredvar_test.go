package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestChainBodyLint_ARequiredFieldFedByAnEmptyDeclaredVarIsAnError(t *testing.T) {
	step := &chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Body:   map[string]any{"name": "${vars.thing_name}", "kind": "KIND_A"},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	}
	c := &chain.Chain{Name: "bodies", Vars: map[string]any{"thing_name": ""}, Steps: []*chain.Step{step}}
	_ = c.Normalize()

	issues := bodyIssues(t, c, libraryRequiring("name"))
	if len(issues) != 1 || issues[0].Severity != chain.SeverityError {
		t.Fatalf("an empty declared var sends nothing for the required field, want one error, got %+v", issues)
	}
	if !strings.Contains(issues[0].Message, "-var thing_name=") {
		t.Fatalf("the error must say how to supply the var, got %q", issues[0].Message)
	}

	c.Vars["thing_name"] = "widget"
	if got := bodyIssues(t, c, libraryRequiring("name")); len(got) != 0 {
		t.Fatalf("a var with a value fills the field, got %+v", got)
	}

	delete(c.Vars, "thing_name")
	if got := bodyIssues(t, c, libraryRequiring("name")); len(got) != 0 {
		t.Fatalf("an undeclared var is refused by run before sending, lint must not call it empty, got %+v", got)
	}
}
