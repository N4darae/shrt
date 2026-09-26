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
	if err == nil {
		t.Fatalf("a chain naming an rpc that does not exist must be refused before sending, allow_fail or not; got status %s", rec.Status)
	}
	if !strings.Contains(err.Error(), "NoSuchRpc") || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("the refusal must name the rpc and say nothing was sent: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.calls) != 0 {
		t.Fatalf("nothing may be sent, the backend received %v", srv.calls)
	}
}

func TestAllowFailDoesNotSwallowAnUnresolvedReference(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := allowFailChain(t, func(s *chain.Step) {
		s.Body = map[string]any{"name": "${env.SHRT_TEST_NEVER_EXPORTED_7F3A}", "kind": "KIND_A", "idempotency_key": "${uuid}"}
	})

	_, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("a chain reading an unset env var must be refused before sending, allow_fail or not, got %v", err)
	}
}

func TestAllowFailDoesNotSwallowARequestTheProtoRejects(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := allowFailChain(t, func(s *chain.Step) {
		s.Body = map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}", "not_a_real_field": 1}
	})

	_, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("a chain whose body does not match the proto message must be refused before sending, allow_fail or not, got %v", err)
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
