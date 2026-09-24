package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAnExportReadBeforeItIsProducedNamesTheStepThatExportsIt(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "early", Steps: []*chain.Step{
		{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${exports.later_id}"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}, Export: map[string]string{"later_id": "id"}},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := rec.Steps[0].Error
	if !strings.Contains(got, "${exports.later_id}") || !strings.Contains(got, "exported by create at step 2") || !strings.Contains(got, "runs later") {
		t.Fatalf("the step must say which step exports later_id and that it runs later, got %q", got)
	}
}

func TestAStepReadBeforeItRunsSaysItRunsLater(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "early", Steps: []*chain.Step{
		{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${create.id}"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := rec.Steps[0].Error
	if !strings.Contains(got, `"create"`) || !strings.Contains(got, "does not run before this step") {
		t.Fatalf("the step must say the step it reads runs later, got %q", got)
	}
}
