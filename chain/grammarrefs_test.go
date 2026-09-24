package chain_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func grammarExampleChain(t *testing.T, headers map[string]string, expect []chain.Expectation) *chain.Chain {
	t.Helper()
	sc := chain.ReferenceExampleScope()
	vars := map[string]any{}
	for name, v := range sc.Vars {
		if s, ok := v.(string); ok && chain.HasReference(s) {
			v = "verbatim"
		}
		vars[name] = v
	}
	export := map[string]string{}
	for name := range sc.Exports {
		export[name] = "id"
	}
	c := &chain.Chain{
		Name: "grammar-refs",
		Vars: vars,
		Steps: []*chain.Step{
			{
				ID:     chain.ReferenceExampleStep,
				Call:   "ThingService/Create",
				Body:   map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"},
				Export: export,
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
			},
			{
				ID:      "use",
				Call:    "ThingService/Fetch",
				Body:    map[string]any{"id": "thing-1"},
				Headers: headers,
				Expect:  append([]chain.Expectation{{Path: "error.code", Equals: "OK"}}, expect...),
			},
		},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func referenceErrors(issues []chain.Issue) []chain.Issue {
	out := []chain.Issue{}
	for _, i := range issues {
		if i.IsError() {
			out = append(out, i)
		}
	}
	return out
}

func TestLintAcceptsEveryReferenceTheGrammarTableDocuments(t *testing.T) {
	if len(chain.ReferenceExamples) == 0 {
		t.Fatal("the GRAMMAR reference table is empty, so this test would check nothing")
	}
	headers := map[string]string{}
	expect := []chain.Expectation{}
	for n, ex := range chain.ReferenceExamples {
		headers[fmt.Sprintf("X-Example-%d", n)] = ex.Ref
		expect = append(expect, chain.Expectation{Path: "name", Equals: ex.Ref})
	}
	c := grammarExampleChain(t, headers, expect)

	if errs := referenceErrors(chain.Lint(c, catalogtest.New())); len(errs) != 0 {
		t.Fatalf("GRAMMAR §2 documents these references and 'shrt run' resolves every one, so lint "+
			"may not reject any of them: %+v", errs)
	}
}

func TestLintRejectsAClockOffsetTheResolverCannotParse(t *testing.T) {
	for _, ref := range []string{"${nowunix+1h}", "${today-x}", "${now+5s}"} {
		c := grammarExampleChain(t, map[string]string{"X-When": ref}, nil)
		errs := referenceErrors(chain.Lint(c, catalogtest.New()))
		if len(errs) != 1 || !strings.Contains(errs[0].Message, "whole number of seconds") {
			t.Errorf("%s dies at run time, so lint must reject it with the resolver's own reason: %+v", ref, errs)
		}
	}
}
