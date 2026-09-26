package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func misspeltRefChain(ref string) *chain.Chain {
	c := &chain.Chain{Name: "typo", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": ref},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}, {Path: "id", Equals: ref}}},
	}}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func TestAReferenceToAFieldTheResponseLacksIsALintError(t *testing.T) {
	issues := chain.Lint(misspeltRefChain("${create.idd}"), catalogtest.New())
	body, expect := false, false
	for _, i := range issues {
		if i.Kind != chain.KindDeadRef || !strings.Contains(i.Message, `"idd"`) {
			continue
		}
		if !i.IsError() {
			t.Fatalf("the descriptor knows create's response has no idd, so the run is guaranteed to die "+
				"on it after create has hit the backend; that is an error, not a warning: %+v", i)
		}
		if strings.Contains(i.Message, "expect") {
			expect = true
		} else {
			body = true
		}
	}
	if !body || !expect {
		t.Fatalf("the misspelt field is read both in the body and in an expect, and both must be reported; got %+v", issues)
	}
}

func TestACorrectResponseReferenceIsNotReported(t *testing.T) {
	for _, i := range chain.Lint(misspeltRefChain("${create.id}"), catalogtest.New()) {
		if i.Kind == chain.KindDeadRef {
			t.Fatalf("id is a field of CreateResponse: %+v", i)
		}
	}
}

func TestResponseRefProblemsNamesTheMisspeltField(t *testing.T) {
	problems := misspeltRefChain("${create.idd}").ResponseRefProblems(catalogtest.New())
	if len(problems) != 2 || !strings.Contains(problems[0], "idd") {
		t.Fatalf("want the body and the expect reference reported, got %v", problems)
	}
	if got := misspeltRefChain("${create.id}").ResponseRefProblems(catalogtest.New()); len(got) != 0 {
		t.Fatalf("a correct reference is no problem, got %v", got)
	}
}
