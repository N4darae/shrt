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
