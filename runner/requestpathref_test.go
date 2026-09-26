package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestARunReadingARequestPathTheEarlierRequestLacksSendsNothing(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "typo-request", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create",
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "fetch", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${steps.create.request.nmae}"}, Expect: okExpect()},
	}})
	_, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err == nil || !strings.Contains(err.Error(), "nmae") || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("create's request declares no nmae, so the run is refused before sending, got %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.calls) != 0 {
		t.Fatalf("nothing may be sent before the refusal, the backend received %v", srv.calls)
	}
}
