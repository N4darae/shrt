package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAResponseFieldExpectationOnAStepExpectedToBeRefusedAtTheTransportIsAnError(t *testing.T) {
	for _, s := range []*chain.Step{
		{ID: "fetch_without_token", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "x"},
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "unauthenticated"}, {Path: "created_at", Equals: "2026"}}},
		{ID: "fetch_with_bad_token", Call: "ThingService/Fetch", Auth: "invalid", Body: map[string]any{"id": "x"},
			Expect: []chain.Expectation{{Path: "transport.http_status", Equals: 401}, {Path: "name", NotEmpty: true}}},
	} {
		got := issuesOfKind(s, chain.KindUnevaluableOnRefusal)
		if len(got) != 1 || got[0].Severity != chain.SeverityError {
			t.Fatalf("%s: want one error, got %v", s.ID, got)
		}
		if !strings.Contains(got[0].Message, "never evaluated") || !strings.Contains(got[0].Message, s.Expect[1].Path) {
			t.Fatalf("%s: the error names the expectation and why: %s", s.ID, got[0].Message)
		}
	}
	answered := &chain.Step{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "x"},
		Expect: []chain.Expectation{{Path: "transport.code", Equals: "ok"}, {Path: "name", NotEmpty: true}}}
	if got := issuesOfKind(answered, chain.KindUnevaluableOnRefusal); len(got) != 0 {
		t.Fatalf("a step expecting transport.code ok has a response to read: %v", got)
	}
}
