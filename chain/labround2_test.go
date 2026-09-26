package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func issuesOfKind(s *chain.Step, kind string) []chain.Issue {
	c := &chain.Chain{Name: "t", Steps: []*chain.Step{s}}
	_ = c.Normalize()
	out := []chain.Issue{}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if i.Kind == kind {
			out = append(out, i)
		}
	}
	return out
}

func TestAllowFailOnAStepWithExpectationsIsReportedAsInert(t *testing.T) {
	got := issuesOfKind(&chain.Step{
		ID: "probe", Call: "ThingService/Fetch", SkipAuth: true, AllowFail: true,
		Expect: []chain.Expectation{{Path: "error.code", Equals: "NOT_FOUND"}},
	}, chain.KindInertAllowFail)
	if len(got) != 1 {
		t.Fatalf("want 1 inert-allow-fail issue, got %v", got)
	}
	msg := got[0].Message
	for _, want := range []string{"does nothing", "NO expect", "transport.code"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message must say %q: %s", want, msg)
		}
	}
	if !chain.IsAssertionQualityIssue(got[0]) {
		t.Error("an inert allow_fail must be promoted by -strict, not silently accepted")
	}
	if p := chain.Promote(got, chain.IsAssertionQualityIssue); p[0].Severity != chain.SeverityError {
		t.Error("-strict did not promote the inert allow_fail warning")
	}
}

func TestAllowFailOnAStepWithoutExpectationsIsNotReportedAsInert(t *testing.T) {
	if got := issuesOfKind(&chain.Step{
		ID: "probe", Call: "ThingService/Fetch", SkipAuth: true, AllowFail: true,
	}, chain.KindInertAllowFail); len(got) != 0 {
		t.Fatalf("allow_fail on a step with no expect is the case it covers, got %v", got)
	}
}

func TestTransportExistsWarningEndsWithTheFixNotTheProblem(t *testing.T) {
	got := issuesOfKind(&chain.Step{
		ID: "probe", Call: "ThingService/Fetch", SkipAuth: true,
		Expect: []chain.Expectation{{Path: "transport.code", Exists: yes()}},
	}, chain.KindUnfailable)
	if len(got) != 1 {
		t.Fatalf("want 1 unfailable issue, got %v", got)
	}
	msg := got[0].Message
	if strings.Contains(msg, "unauthenticated, so it passes") {
		t.Fatalf("the example fix is followed by the problem clause again: %s", msg)
	}
	fix := strings.Index(msg, "Assert the outcome's VALUE")
	problem := strings.Index(msg, "so it passes whatever the server answers")
	if fix < 0 || problem < 0 || fix < problem {
		t.Fatalf("want the problem stated first and the fix last: %s", msg)
	}
	if !strings.HasSuffix(msg, "transport.code equals: unauthenticated") {
		t.Fatalf("the message must end on the fix: %s", msg)
	}
}

func TestEnvelopeExistsWarningNamesTheEnvelopeCodeToAssert(t *testing.T) {
	got := issuesOfKind(&chain.Step{
		ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true,
		Expect: []chain.Expectation{{Path: "error.code", NotEmpty: true}},
	}, chain.KindUnfailable)
	if len(got) != 1 {
		t.Fatalf("want 1 unfailable issue, got %v", got)
	}
	if !strings.Contains(got[0].Message, "error.code equals: OK") {
		t.Fatalf("want the envelope fix to name the path and its OK value: %s", got[0].Message)
	}
}

func TestMissingVarsListsOnlyUndeclaredUnsuppliedVars(t *testing.T) {
	c := &chain.Chain{Name: "t", Vars: map[string]any{"declared": "x"}, Steps: []*chain.Step{
		{ID: "a", Call: "ThingService/Create", Body: map[string]any{
			"name": "${vars.declared}-${vars.batch}-${vars.given}-${vars.tag}", "kind": "${vars.kind}"}},
	}}
	got := c.MissingVars(map[string]any{"given": "y"})
	if strings.Join(got, ",") != "batch,kind" {
		t.Fatalf("MissingVars = %v, want [batch kind]: an undeclared tag is fresh per run", got)
	}
}
