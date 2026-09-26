package runner_test

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func deadTarget(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return "http://" + addr
}

func TestKeepGoingStopsSendingOnceTheTargetIsUnreachable(t *testing.T) {
	target := deadTarget(t)
	deps, err := runner.Build(context.Background(), testConfig(target), catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, Auth: deps.Bindings}
	c := normalized(t, &chain.Chain{Name: "dead", Steps: []*chain.Step{
		{ID: "a", Call: "ThingService/Create", Body: map[string]any{"name": "x", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "b", Call: "ThingService/Create", Body: map[string]any{"name": "y", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "c", Call: "ThingService/Create", Body: map[string]any{"name": "z", "kind": "KIND_A"}, Expect: okExpect()},
	}})
	seen := 0
	r.OnStep = func(*runner.StepRecord) { seen++ }
	rec, err := r.Run(context.Background(), c, runner.Options{KeepGoing: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Steps[0].Status != runner.StatusError || rec.Status != runner.StatusError {
		t.Fatalf("the first step dies on the dial, got %s / run %s", rec.Steps[0].Status, rec.Status)
	}
	for _, sr := range rec.Steps[1:] {
		if sr.Status != runner.StatusSkipped || sr.Request != nil || !sr.NotSentUnreachable() {
			t.Fatalf("step %s must be skipped unsent behind the dead target, got %s", sr.ID, sr.Status)
		}
		if !strings.Contains(sr.Error, target) || !strings.Contains(sr.Error, "unreachable") {
			t.Fatalf("the skip must name the unreachable target, got %q", sr.Error)
		}
	}
	if seen != 3 || len(rec.FailedSteps) != 3 {
		t.Fatalf("every step is still recorded and reported, seen=%d failed=%v", seen, rec.FailedSteps)
	}
	if strings.Count(rec.Failure, "unreachable") != 1 {
		t.Fatalf("the summary names the dead target once, not per step: %q", rec.Failure)
	}
}
