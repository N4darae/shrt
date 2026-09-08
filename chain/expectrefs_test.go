package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func chainExpecting(t *testing.T, e chain.Expectation) *chain.Chain {
	t.Helper()
	c := &chain.Chain{
		Name: "expect-refs",
		Vars: map[string]any{"wanted": "abc"},
		Steps: []*chain.Step{{
			ID: "create", Call: "ThingService/Create", SkipAuth: true,
			Body:   map[string]any{"name": "widget"},
			Expect: []chain.Expectation{e},
		}},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func lintMessages(t *testing.T, c *chain.Chain) []string {
	t.Helper()
	out := []string{}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if i.Severity == chain.SeverityError {
			out = append(out, i.Message)
		}
	}
	return out
}

// A reference in a comparison VALUE is now legal and resolves at run time — that is what lets a
// chain assert a money invariant across two steps. A reference in PATH stays an error: a path names
// a location in this step's own response, so there is no value for it to resolve to.
//
// The previous version of this test asserted the opposite for all four fields, citing
// docs/chain-spec.md:70, and it was correct until 2026-09-09. It is kept in inverted form rather
// than deleted because the half that survives — path — is the half a reader is most likely to try.
func TestLintAcceptsAReferenceInAnExpectationValue(t *testing.T) {
	cases := map[string]chain.Expectation{
		"equals":    {Path: "id", Equals: "${vars.wanted}"},
		"not_equal": {Path: "id", NotEqual: "${vars.wanted}"},
		"contains":  {Path: "id", Contains: "${vars.wanted}"},
	}
	for name, e := range cases {
		msgs := lintMessages(t, chainExpecting(t, e))
		if len(msgs) != 0 {
			t.Errorf("%s: a ${...} in a comparison value must LINT CLEAN — it resolves at run time, "+
				"and without it no chain can assert after == before. got errors: %v", name, msgs)
		}
	}
}

func TestLintRejectsAReferenceInAnExpectationPath(t *testing.T) {
	msgs := lintMessages(t, chainExpecting(t, chain.Expectation{Path: "${vars.wanted}", NotEmpty: true}))
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "${") && strings.Contains(m, "path") {
			found = true
		}
	}
	if !found {
		t.Errorf("a ${...} in an expect PATH must stay a lint ERROR — a path names a location in this "+
			"step's own response, not a value. got errors: %v", msgs)
	}
}

func TestLintRejectsAnExpectationReferencingALaterStep(t *testing.T) {
	c := &chain.Chain{
		Name: "expect-forward-ref",
		Steps: []*chain.Step{
			{
				ID: "first", Call: "ThingService/Create", SkipAuth: true,
				Body:   map[string]any{"name": "widget"},
				Expect: []chain.Expectation{{Path: "id", Equals: "${steps.second.response.id}"}},
			},
			{
				ID: "second", Call: "ThingService/Create", SkipAuth: true,
				Body: map[string]any{"name": "widget"},
			},
		},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	msgs := lintMessages(t, c)
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "second") && strings.Contains(m, "does not run before") {
			found = true
		}
	}
	if !found {
		t.Errorf("an expect referencing a LATER step must be a lint ERROR — it would resolve to nothing "+
			"and the assertion would compare against emptiness while looking deliberate. got errors: %v", msgs)
	}
}

func TestLintAcceptsAnExpectationWithNoReference(t *testing.T) {
	msgs := lintMessages(t, chainExpecting(t, chain.Expectation{Path: "error.code", Equals: "OK"}))
	if len(msgs) != 0 {
		t.Errorf("a plain expectation must lint clean, got %v", msgs)
	}
}

func TestLintAllowsADollarThatIsNotAReference(t *testing.T) {
	msgs := lintMessages(t, chainExpecting(t, chain.Expectation{Path: "id", Equals: "costs $5 today"}))
	if len(msgs) != 0 {
		t.Errorf("a bare $ is not a reference and must not be flagged, got %v", msgs)
	}
}
