package runner_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestARunWithAReferenceToNoStepSendsNothing(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "typo-ref", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create",
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "fetch", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${craete.id}"}, Expect: okExpect()},
	}})
	_, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err == nil || !strings.Contains(err.Error(), "craete") || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("a reference to a step that does not exist must be refused before sending, got %v", err)
	}
	if !strings.Contains(err.Error(), `did you mean "create"`) {
		t.Fatalf("the refusal should suggest the closest step, got %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.calls) != 0 {
		t.Fatalf("nothing may be sent before the refusal, the backend received %v", srv.calls)
	}
}

func TestARunReadingAnUnsetEnvVarInAStepBodySendsNothing(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	t.Setenv("SHRT_TEST_PREFLIGHT_UNSET_9C2E", "x")
	os.Unsetenv("SHRT_TEST_PREFLIGHT_UNSET_9C2E")

	c := normalized(t, &chain.Chain{Name: "unset-env", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create",
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "fetch", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${env.SHRT_TEST_PREFLIGHT_UNSET_9C2E}"}, Expect: okExpect()},
	}})
	_, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err == nil || !strings.Contains(err.Error(), "SHRT_TEST_PREFLIGHT_UNSET_9C2E") || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("an unset env var a step reads must be refused before sending, got %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.calls) != 0 {
		t.Fatalf("nothing may be sent before the refusal, the backend received %v", srv.calls)
	}
}

func TestARunNamesAStepOnceAndSuggestsAStepNamedByOneWordOfTheReference(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "word-ref", Steps: []*chain.Step{
		{ID: "customer", Call: "ThingService/Create",
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "fetch", Call: "ThingService/Create",
			Body: map[string]any{"name": "${create_customer.id}", "kind": "KIND_A", "meta": map[string]any{"trace_id": "${create_customer.id}"}}, Expect: okExpect()},
	}})
	_, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err == nil || !strings.Contains(err.Error(), `did you mean "customer"?`) {
		t.Fatalf("a step named by one word of the reference is suggested, got %v", err)
	}
	if strings.Count(err.Error(), `"fetch" (step 2)`) != 1 {
		t.Fatalf("the step is named once, got %v", err)
	}
}
