package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func allowFailChain(t *testing.T, mutate func(*chain.Step)) *chain.Chain {
	t.Helper()
	c := testChain()
	c.Steps[0].AllowFail = true
	mutate(c.Steps[0])
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAllowFailToleratesTheCallItselfBeingRefused(t *testing.T) {
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
		t.Fatalf("allow_fail must tolerate the backend refusing the call, which is what it is for: %s", rec.Failure)
	}
	if len(rec.Steps) != 2 {
		t.Fatalf("the chain should have continued past the tolerated step, got %d step records", len(rec.Steps))
	}
	if rec.Steps[0].Status != runner.StatusFailed {
		t.Fatalf("the tolerated step should still be recorded as failed, got %s", rec.Steps[0].Status)
	}
}

func TestAllowFailDoesNotSwallowAnRPCThatDoesNotExist(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := allowFailChain(t, func(s *chain.Step) {
		s.Call = "ThingService/NoSuchRpc"
	})

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("a chain naming an rpc that does not exist reported PASSED: allow_fail must not cover a call that never reached the backend")
	}
	if rec.Status != runner.StatusError {
		t.Fatalf("record status = %s, want %s", rec.Status, runner.StatusError)
	}
	if !strings.Contains(rec.Failure, "allow_fail does not cover this") {
		t.Fatalf("failure does not explain why allow_fail did not apply: %q", rec.Failure)
	}
}

func TestAllowFailDoesNotSwallowAnUnresolvedReference(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := allowFailChain(t, func(s *chain.Step) {
		s.Body = map[string]any{"name": "${vars.never_declared}", "kind": "KIND_A", "idempotency_key": "${uuid}"}
	})

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("a chain with an unresolved reference reported PASSED under allow_fail")
	}
	if rec.Status != runner.StatusError {
		t.Fatalf("record status = %s, want %s", rec.Status, runner.StatusError)
	}
}

func TestAllowFailDoesNotSwallowARequestTheProtoRejects(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := allowFailChain(t, func(s *chain.Step) {
		s.Body = map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}", "not_a_real_field": 1}
	})

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("a chain whose body does not match the proto message reported PASSED under allow_fail")
	}
	if rec.Status != runner.StatusError {
		t.Fatalf("record status = %s, want %s", rec.Status, runner.StatusError)
	}
}

func TestAllowFailDoesNotWaiveAnExpectationTheAuthorWrote(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := allowFailChain(t, func(s *chain.Step) {
		s.Expect = []chain.Expectation{{Path: "id", Equals: "deliberately-wrong"}}
	})

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("a chain whose only assertion failed reported PASSED because the step carried allow_fail. " +
			"allow_fail tolerates the CALL being refused; an expectation is the author's claim about what " +
			"happened, and waiving it turns every failure probe into decoration — and lets the run be confirmed")
	}
	if !strings.Contains(rec.Failure, "not an expectation you wrote being") {
		t.Errorf("the failure does not explain what allow_fail does and does not cover: %q", rec.Failure)
	}
}

func TestAllowFailStillToleratesARefusalTheStepCorrectlyPredicted(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := allowFailChain(t, func(s *chain.Step) {
		s.Expect = []chain.Expectation{{Path: "error.code", Equals: "OK"}, {Path: "id", NotEmpty: true}}
	})

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("a step whose assertions all held must still pass under allow_fail: %s", rec.Failure)
	}
}
