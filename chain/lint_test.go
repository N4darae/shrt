package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func lintOf(t *testing.T, steps ...*chain.Step) []chain.Issue {
	t.Helper()
	for _, st := range steps {
		if len(st.Expect) == 0 {
			st.Expect = []chain.Expectation{{Path: "error.code", Equals: "OK"}}
		}
	}
	c := &chain.Chain{Name: "t", Steps: steps}
	if err := c.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	return chain.Lint(c, catalogtest.New())
}

func TestLintAcceptsAValidChain(t *testing.T) {
	issues := lintOf(t,
		&chain.Step{ID: "create", Call: "ThingService/Create",
			Body:   map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"},
			Export: map[string]string{"thing_id": "id"}},
		&chain.Step{ID: "fetch", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${create.id}"}},
	)
	if len(issues) != 0 {
		t.Fatalf("want no issues, got %v", issues)
	}
}

func TestLintCatchesABareReferenceToNothingDeclared(t *testing.T) {
	issues := lintOf(t,
		&chain.Step{ID: "create", Call: "ThingService/Create",
			Body:   map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"},
			Export: map[string]string{"thing_id": "id"}},
		&chain.Step{ID: "fetch", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${thing_i}"}},
	)
	var found bool
	for _, i := range issues {
		if i.Severity == chain.SeverityError && strings.Contains(i.Message, "thing_i") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a bare reference to an undeclared export/step must be a lint error, got %v", issues)
	}
}

func TestLintAcceptsABareExportReference(t *testing.T) {
	issues := lintOf(t,
		&chain.Step{ID: "create", Call: "ThingService/Create",
			Body:   map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"},
			Export: map[string]string{"thing_id": "id"}},
		&chain.Step{ID: "fetch", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${thing_id}"}},
	)
	if len(issues) != 0 {
		t.Fatalf("a bare reference to a real export from an earlier step must lint clean, got %v", issues)
	}
}

func TestLintCatchesUnknownRPC(t *testing.T) {
	issues := lintOf(t, &chain.Step{ID: "x", Call: "ThingService/Vanish"})
	if len(issues) != 1 || issues[0].Severity != chain.SeverityError {
		t.Fatalf("want one error, got %v", issues)
	}
}

func TestLintCatchesUnknownFieldEvenBehindAReference(t *testing.T) {
	issues := lintOf(t, &chain.Step{ID: "create", Call: "ThingService/Create",
		Body: map[string]any{"nope": "${vars.x}"}})
	found := false
	for _, i := range issues {
		if i.Severity == chain.SeverityError && strings.Contains(i.Message, "nope") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want an unknown-field error naming nope, got %v", issues)
	}
}

func TestLintCatchesForwardStepReference(t *testing.T) {
	issues := lintOf(t,
		&chain.Step{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${create.id}"}},
		&chain.Step{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w"}},
	)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "does not run before") {
		t.Fatalf("want a forward-reference error, got %v", issues)
	}
}

func TestLintWarnsOnAnExportThatCannotExist(t *testing.T) {
	issues := lintOf(t, &chain.Step{ID: "create", Call: "ThingService/Create",
		Body: map[string]any{"name": "w"}, Export: map[string]string{"x": "not_a_field"}})
	if len(issues) != 1 || issues[0].Severity != chain.SeverityWarn {
		t.Fatalf("want one warning, got %v", issues)
	}
}

func TestNormalizeRejectsDuplicateStepIDs(t *testing.T) {
	c := &chain.Chain{Name: "t", Steps: []*chain.Step{
		{ID: "a", Call: "ThingService/Create"},
		{ID: "a", Call: "ThingService/Fetch"},
	}}
	if err := c.Normalize(); err == nil {
		t.Fatal("duplicate step ids must be rejected")
	}
}

func TestLintScansHeaderReferences(t *testing.T) {
	issues := lintOf(t,
		&chain.Step{ID: "fetch", Call: "ThingService/Fetch",
			Body:    map[string]any{"id": "x"},
			Headers: map[string]string{"X-Ref": "${create.id}"}},
		&chain.Step{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w"}},
	)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "does not run before") {
		t.Fatalf("a forward reference in headers must be caught, got %v", issues)
	}
}
