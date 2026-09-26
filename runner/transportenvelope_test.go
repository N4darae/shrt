package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAFailedTransportAssertionOnAnAnsweredCallSaysWhatTheEnvelopeSaid(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.createRefusal = "out_of_stock"

	c := normalized(t, &chain.Chain{Name: "refused", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "invalid_argument"}}},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	line := rec.Steps[0].Expect[0].String()
	for _, want := range []string{"got=ok", "error.code = out_of_stock", `message="refused"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("got=ok alone hides that the backend refused in-band; the line lacks %q: %q", want, line)
		}
	}
}
