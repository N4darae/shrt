package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func refusedCreateChain(t *testing.T) *chain.Chain {
	t.Helper()
	return normalized(t, &chain.Chain{Name: "refused", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "fetch_one", Call: "ThingService/Fetch", Body: map[string]any{"id": "${create.id}"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "fetch_two", Call: "ThingService/Fetch", Body: map[string]any{"id": "${create.id}"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
	}})
}

func TestTheRunFailureNamesTheFirstFailingExpectation(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.createRefusal = "out_of_stock"

	rec, err := newRunner(t, srv).Run(context.Background(), refusedCreateChain(t), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{`step "create": expectation failed`, "error.code", "want=OK", "got=out_of_stock", `message="refused"`} {
		if !strings.Contains(rec.Failure, want) {
			t.Fatalf("the failure line is what -quiet shows, so it must say what failed; lacks %q: %q", want, rec.Failure)
		}
	}
}

func TestAKeepGoingSkipReasonIsGivenOnce(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.createRefusal = "out_of_stock"

	rec, err := newRunner(t, srv).Run(context.Background(), refusedCreateChain(t), runner.Options{KeepGoing: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if n := strings.Count(rec.Failure, "was refused in-band"); n != 1 {
		t.Fatalf("both skipped steps read the same refused step, so the reason is said once, got %d times:\n%s", n, rec.Failure)
	}
	if !strings.Contains(rec.Failure, `"fetch_two"`) {
		t.Fatalf("the second skipped step must still be listed:\n%s", rec.Failure)
	}
}
