package runner_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

// A cross-step assertion is the whole point of resolving ${...} inside an expect: without it no
// chain can state "after == before", "side A == side B", or "give + fees == get + margin", and
// every conservation check has to be a hand-typed literal encoding its author's belief.
//
// The discriminator is the RECORDED Want, not merely whether the step passed. An unresolved
// reference would be compared as the literal characters "${steps.first.response.id}", which also
// fails an equals against a real id — so a red step proves nothing on its own. Asserting that Want
// holds the RESOLVED value is what separates "the reference resolved" from "the comparison happened
// to disagree".
func TestAnExpectationResolvesAReferenceToAnEarlierStep(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := &chain.Chain{
		Name: "expect-cross-step",
		Steps: []*chain.Step{
			{
				ID: "login", Call: "AuthService/Login", SkipAuth: true,
				Body: map[string]any{"username": "alice", "password": "hunter2"},
			},
			{
				ID: "first", Call: "ThingService/Create",
				Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
			},
			{
				ID: "second", Call: "ThingService/Create",
				Body: map[string]any{"name": "widget", "kind": "KIND_A"},
				Expect: []chain.Expectation{
					{Path: "error.code", Equals: "OK"},
					{Path: "id", NotEqual: "${steps.first.response.id}"},
				},
			},
		},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("two Creates return different ids, so not_equal must PASS: %s", rec.Failure)
	}

	first, ok := rec.Step("first")
	if !ok {
		t.Fatal("no record for step first")
	}
	firstID := fmt.Sprintf("%v", firstResponseID(t, first))

	second, ok := rec.Step("second")
	if !ok {
		t.Fatal("no record for step second")
	}
	var checked bool
	for _, e := range second.Expect {
		if e.Path != "id" {
			continue
		}
		checked = true
		got := fmt.Sprintf("%v", e.Want)
		if got == "${steps.first.response.id}" {
			t.Fatalf("Want was recorded as the RAW reference %q — it was compared as literal text, "+
				"which is the behaviour this change exists to remove", got)
		}
		if got != firstID {
			t.Errorf("Want = %q, want the resolved id %q from step first", got, firstID)
		}
	}
	if !checked {
		t.Error("step second recorded no expectation on path id")
	}
}

func TestAnExpectationReferencingAMissingValueFailsLoudly(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := &chain.Chain{
		Name: "expect-missing-ref",
		Steps: []*chain.Step{
			{
				ID: "login", Call: "AuthService/Login", SkipAuth: true,
				Body: map[string]any{"username": "alice", "password": "hunter2"},
			},
			{
				ID: "create", Call: "ThingService/Create",
				Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
				Expect: []chain.Expectation{{Path: "id", Equals: "${vars.never_set}"}},
			},
		},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err == nil && rec.Passed() {
		t.Fatal("an expect naming a value that does not exist must NOT pass — a silently empty " +
			"comparison is exactly the assertion-that-cannot-fail this change must not introduce")
	}
}

func firstResponseID(t *testing.T, s *runner.StepRecord) any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(s.Response, &decoded); err != nil {
		t.Fatalf("decode step first response: %v", err)
	}
	v, ok := decoded["id"]
	if !ok {
		t.Fatalf("step first response has no id: %v", decoded)
	}
	return v
}
