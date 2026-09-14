package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAllowFailDoesNotSwallowAStepWhoseAssertionsNeverRan(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := allowFailChain(t, func(s *chain.Step) {
		s.Call = "PartnerService/FetchMine"
		s.Body = map[string]any{"id": "thing-1"}
		s.Export = nil
		s.Expect = []chain.Expectation{
			{Path: "error.code", Equals: "OK"},
			{Path: "name", NotEmpty: true},
		}
	})
	c.Steps[1].Body = map[string]any{"id": "thing-1"}
	c.Steps[1].Expect = []chain.Expectation{{Path: "error.code", Equals: "OK"}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatalf("chain PASSED while the call was refused and NEITHER declared assertion was evaluated. "+
			"allow_fail tolerates a refusal, not an assertion going unchecked — anything reading the exit "+
			"code sees success. failure=%q", rec.Failure)
	}
	sr := rec.Steps[0]
	if len(sr.Expect) != 2 {
		t.Fatalf("want 2 assertions recorded as unevaluated, got %d — a record that simply omits them "+
			"cannot tell a reader the step proved nothing", len(sr.Expect))
	}
	for _, e := range sr.Expect {
		if e.Passed {
			t.Errorf("assertion on %q recorded as passed with no response body", e.Path)
		}
		if !strings.Contains(e.Detail, "never ran") {
			t.Errorf("assertion on %q does not say why it went unevaluated: %q", e.Path, e.Detail)
		}
	}
}

func TestAllowFailStillToleratesARefusalTheStepDeclaredNoAssertionsFor(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := allowFailChain(t, func(s *chain.Step) {
		s.Call = "PartnerService/FetchMine"
		s.Body = map[string]any{"id": "thing-1"}
		s.Export = nil
		s.Expect = nil
	})
	c.Steps[1].Body = map[string]any{"id": "thing-1"}
	c.Steps[1].Expect = []chain.Expectation{{Path: "error.code", Equals: "OK"}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("a step that asserted nothing about the refusal is exactly what allow_fail covers, and "+
			"must still be tolerated: %s", rec.Failure)
	}
}
