package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func lintOne(t *testing.T, path string) []chain.Issue {
	t.Helper()
	cat := catalogtest.Batch()
	c := &chain.Chain{
		Name: "probe",
		Steps: []*chain.Step{{
			ID:     "preview",
			Call:   "BatchService/Preview",
			Body:   map[string]any{"lines": []any{"a"}},
			Expect: []chain.Expectation{{Path: path, Equals: "x"}},
		}},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return chain.Lint(c, cat)
}

func TestExpectThroughARepeatedFieldWithoutAnIndexIsReported(t *testing.T) {
	issues := lintOne(t, "results.error.code")
	found := ""
	for _, i := range issues {
		if i.Kind == chain.KindUnreachable {
			found = i.Message
		}
	}
	if found == "" {
		t.Fatalf("a path reading through the repeated field 'results' with no index can never match, "+
			"and every check upstream of the run passes it: lint resolves the field names one by one "+
			"and never asks whether a list was crossed. It fails only against live traffic, as "+
			"'path not present in response'. issues=%v", issues)
	}
	for _, i := range issues {
		if i.Kind == chain.KindUnreachable && !i.IsError() {
			t.Errorf("an un-indexed path through a list is a path the response cannot carry, so it is an error: %v", i)
		}
	}
	if !strings.Contains(found, "results.0.error.code") {
		t.Errorf("the message should name the path that does work: %q", found)
	}
}

func TestAnIndexedPathThroughARepeatedFieldIsFine(t *testing.T) {
	for _, i := range lintOne(t, "results.0.error.code") {
		if i.Kind == chain.KindUnreachable {
			t.Fatalf("results.0.error.code is the correct form and must lint clean: %s", i.Message)
		}
	}
}

func TestTheRepeatedFieldItselfIsFine(t *testing.T) {
	for _, i := range lintOne(t, "results") {
		if i.Kind == chain.KindUnreachable {
			t.Fatalf("the list itself is a real path — not_empty on it is how a chain says the batch "+
				"came back with rows: %s", i.Message)
		}
	}
}

func TestExistsFalseThroughARepeatedFieldWithoutAnIndexCannotFail(t *testing.T) {
	no := false
	c := &chain.Chain{
		Name: "probe",
		Steps: []*chain.Step{{
			ID:   "preview",
			Call: "BatchService/Preview",
			Body: map[string]any{"lines": []any{"a"}},
			Expect: []chain.Expectation{
				{Path: "results.0.error.code", Equals: "OK"},
				{Path: "results.error.message", Exists: &no},
			},
		}},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	for _, i := range chain.Lint(c, catalogtest.Batch()) {
		if strings.Contains(i.Message, "results.error.message") {
			if !i.IsError() || i.Kind != chain.KindUnfailable {
				t.Fatalf("an un-indexed path through a list is never present, so exists: false on it passes "+
					"on every response and must be an unfailable-assertion error: %+v", i)
			}
			return
		}
	}
	t.Fatal("exists: false on an un-indexed path through a list was not reported")
}
