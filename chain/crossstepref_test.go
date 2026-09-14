package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func crossStepIssues(t *testing.T, body map[string]any, export map[string]string) []chain.Issue {
	t.Helper()
	c := &chain.Chain{
		Name: "crossstep",
		Steps: []*chain.Step{
			{
				ID:     "create",
				Call:   "ThingService/Create",
				Body:   map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"},
				Export: export,
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
			},
			{
				ID:     "fetch",
				Call:   "ThingService/Fetch",
				Body:   body,
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
			},
		},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	out := []chain.Issue{}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if strings.Contains(i.Message, "cannot produce it") {
			out = append(out, i)
		}
	}
	return out
}

func TestLintFlagsACrossStepReferenceToAPathTheProducerCannotReturn(t *testing.T) {
	issues := crossStepIssues(t, map[string]any{"id": "${create.thing.id}"}, nil)
	if len(issues) != 1 {
		t.Fatalf("want the unreachable producer path reported, got %v", issues)
	}
	if !strings.Contains(issues[0].Message, "thing.id") {
		t.Errorf("the warning does not name the path: %v", issues[0])
	}
	if !strings.Contains(issues[0].Message, "exports.") {
		t.Errorf("the warning does not mention the export form, which is the thing it is confused with: %v", issues[0])
	}
}

func TestLintAcceptsEveryReferenceFormThatCanActuallyResolve(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		exp  map[string]string
	}{
		{"bare step id", map[string]any{"id": "${create.id}"}, nil},
		{"steps prefix", map[string]any{"id": "${steps.create.id}"}, nil},
		{"steps response prefix", map[string]any{"id": "${steps.create.response.id}"}, nil},
		{"request side", map[string]any{"id": "${steps.create.request.name}"}, nil},
		{"export by name", map[string]any{"id": "${exports.thing_id}"}, map[string]string{"thing_id": "id"}},
		{"bare export", map[string]any{"id": "${thing_id}"}, map[string]string{"thing_id": "id"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if issues := crossStepIssues(t, c.body, c.exp); len(issues) != 0 {
				t.Fatalf("a resolvable reference was reported as unreachable: %v", issues)
			}
		})
	}
}

func TestACrossStepReferenceIsAWarningNotAnError(t *testing.T) {
	issues := crossStepIssues(t, map[string]any{"id": "${create.thing.id}"}, nil)
	if len(issues) == 0 {
		t.Fatal("nothing reported")
	}
	if issues[0].Severity != chain.SeverityWarn {
		t.Errorf("severity = %q, want warn: a response can legitimately outrun a stale descriptor, "+
			"so this must not block a chain that is actually correct", issues[0].Severity)
	}
}
