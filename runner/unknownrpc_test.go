package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestARunWithAnUnknownRPCSendsNothing(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "unknown-rpc", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create",
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "typo", Call: "ThingService/Craete",
			Body: map[string]any{"name": "widget"}, Expect: okExpect()},
	}})
	_, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err == nil || !strings.Contains(err.Error(), "typo") || !strings.Contains(err.Error(), "Craete") {
		t.Fatalf("a chain calling an rpc the catalog does not have must be refused naming the step, got %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.calls) != 0 {
		t.Fatalf("nothing may be sent before the refusal, the backend received %v", srv.calls)
	}
}
