package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestKeepGoingSendsAStepThatReadsAFailedStepOnlyInAnExpectation(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "keep-going-expect", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create",
			Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "id", Equals: "deliberately-wrong"}}},
		{ID: "create_other", Call: "ThingService/Create",
			Body: map[string]any{"name": "gadget", "kind": "KIND_A"},
			Expect: []chain.Expectation{
				{Path: "id", NotEqual: "${create.id}"},
				{Path: "id", NotEmpty: true},
			}},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{KeepGoing: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	other := rec.Steps[1]
	if other.Status == runner.StatusSkipped {
		t.Fatalf("create_other's body does not read create, so it must be sent, got %s: %s", other.Status, other.Error)
	}
	if len(other.Expect) != 2 {
		t.Fatalf("want both expectations recorded, got %+v", other.Expect)
	}
	held, own := other.Expect[0], other.Expect[1]
	if held.Rule != "unevaluated" || held.Passed || !strings.Contains(held.Detail, `"create"`) {
		t.Fatalf("the expectation reading the failed step must be unevaluated and name it, got %+v", held)
	}
	if !own.Passed {
		t.Fatalf("the expectation on the step's own response still runs, got %+v", own)
	}
}

func TestAFailedStepLeadsWithItsFailedExpectationNotTheExportItCouldNotRead(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "export-after-failure", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create",
			Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "id", Equals: "deliberately-wrong"}},
			Export: map[string]string{"thing_ref": "no_such_field"}},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := rec.Steps[0].Error
	if !strings.HasPrefix(got, "expectation failed: id want=deliberately-wrong") || !strings.Contains(got, `export "thing_ref"`) {
		t.Fatalf("the cause is the failed expectation and must come first, got %q", got)
	}
}

func TestKeepGoingSendsAStepReadingAFieldTheFailedStepDidNotFailOn(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "keep-going-sound-field", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create",
			Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "id", NotEmpty: true}, {Path: "name", Equals: "deliberately-wrong"}}},
		{ID: "fetch_by_id", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${create.id}"}, Expect: okExpect()},
		{ID: "fetch_by_name", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${create.name}"}, Expect: okExpect()},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{KeepGoing: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Steps[1].Status == runner.StatusSkipped {
		t.Fatalf("create failed on name, not id, so a step reading its id is sent: %s", rec.Steps[1].Error)
	}
	if rec.Steps[2].Status != runner.StatusSkipped {
		t.Fatalf("a step reading the very field that failed is still held back, got %s", rec.Steps[2].Status)
	}
}
