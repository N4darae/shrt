package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func expectPathIssues(t *testing.T, expect []chain.Expectation) []chain.Issue {
	t.Helper()
	c := &chain.Chain{
		Name: "expectpaths",
		Steps: []*chain.Step{{
			ID:     "fetch",
			Call:   "ThingService/Fetch",
			Body:   map[string]any{"id": "thing-1"},
			Expect: expect,
		}},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	out := []chain.Issue{}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if strings.Contains(i.Message, "can never be present") {
			out = append(out, i)
		}
	}
	return out
}

func TestLintFlagsAnExpectPathTheResponseCannotHave(t *testing.T) {
	issues := expectPathIssues(t, []chain.Expectation{
		{Path: "error.code", Equals: "OK"},
		{Path: "no_such_field", Equals: "x"},
	})
	if len(issues) != 1 {
		t.Fatalf("want exactly the bogus path reported, got %v", issues)
	}
	if !strings.Contains(issues[0].Message, "no_such_field") {
		t.Errorf("the warning does not name the path: %v", issues[0])
	}
	if !issues[0].IsError() {
		t.Error("an expect path the response message has no field for is an ERROR: it cannot pass on a " +
			"well-formed response, and a descriptor that lags the backend is caught by the freshness gate, " +
			"not by tolerating typos here")
	}
	if strings.Contains(issues[0].Message, "exists: false") {
		t.Errorf("the message must not suggest exists: false, which on this path passes whatever the "+
			"server answers: %q", issues[0].Message)
	}
}

func TestLintSaysNothingAboutARealResponsePath(t *testing.T) {
	issues := expectPathIssues(t, []chain.Expectation{
		{Path: "error.code", Equals: "OK"},
		{Path: "name", Equals: "widget"},
		{Path: "id", NotEmpty: true},
	})
	if len(issues) != 0 {
		t.Fatalf("real response paths were reported as unreachable: %v", issues)
	}
}

func TestLintDoesNotFlagAKeyInsideAProtoMap(t *testing.T) {
	c := &chain.Chain{
		Name: "mapkeys",
		Steps: []*chain.Step{{
			ID:   "place",
			Call: "OrderService/PlaceOrder",
			Expect: []chain.Expectation{
				{Path: "labels.anything_at_all", Equals: "x"},
				{Path: "id_order", NotEmpty: true},
			},
		}},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	for _, i := range chain.Lint(c, catalogtest.Rich()) {
		if strings.Contains(i.Message, "can never be present") {
			t.Fatalf("a map key was reported as an unreachable path — every key is valid in a map, "+
				"and treating one as a typo is the false positive that makes a warning worth ignoring: %v", i)
		}
	}
}

func TestLintRefusesExistsFalseOnAPathTheResponseCannotCarry(t *testing.T) {
	no := false
	c := &chain.Chain{
		Name: "absent-typo",
		Steps: []*chain.Step{{
			ID:     "create",
			Call:   "ThingService/Create",
			Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}, {Path: "idd", Exists: &no}},
		}},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	var found *chain.Issue
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if strings.Contains(i.Message, `"idd"`) {
			found = &i
		}
	}
	if found == nil || !found.IsError() {
		t.Fatalf("exists: false on a field the response message does not have is true of every response, "+
			"so the step passes whatever the server does — a misspelt field name is a green that proves "+
			"nothing. It must be a lint error: %+v", found)
	}
	if found.Kind != chain.KindUnfailable {
		t.Errorf("kind = %q, want %q: this assertion cannot fail, which is the fault", found.Kind, chain.KindUnfailable)
	}
}

func TestLintAllowsExistsFalseOnAFieldTheResponseCanCarry(t *testing.T) {
	no := false
	issues := expectPathIssues(t, []chain.Expectation{
		{Path: "error.code", Equals: "OK"},
		{Path: "name", Exists: &no},
	})
	if len(issues) != 0 {
		t.Fatalf("asserting a real field is absent is the legitimate use of exists: false: %v", issues)
	}
}
