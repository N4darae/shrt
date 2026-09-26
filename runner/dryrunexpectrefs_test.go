package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func dryRunChain(t *testing.T, name string, expect []chain.Expectation) *runner.Record {
	t.Helper()
	c := &chain.Chain{
		Name: name,
		Steps: []*chain.Step{
			{
				ID: "first", Call: "ThingService/Create",
				Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
				Export: map[string]string{"alias_only": "id"},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
			},
			{
				ID: "second", Call: "ThingService/Create",
				Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
				Expect: expect,
			},
		},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer()
	defer srv.Close()
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{DryRun: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return rec
}

func dryRunRefusal(t *testing.T, name string, expect []chain.Expectation) error {
	t.Helper()
	c := &chain.Chain{Name: name, Steps: []*chain.Step{
		{ID: "first", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A"},
			Export: map[string]string{"alias_only": "id"}, Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "second", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: expect},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer()
	defer srv.Close()
	_, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{DryRun: true})
	return err
}

func TestADryRunRefusesAnExpectationReferencingAFieldNoResponseCarries(t *testing.T) {
	err := dryRunRefusal(t, "dry-bad-expect-ref", []chain.Expectation{
		{Path: "error.code", Equals: "OK"},
		{Path: "id", Equals: "${first.field_that_cannot_exist}"},
	})
	if err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("a dry run must refuse a reference no response can satisfy; the live run is not the "+
			"first place to learn this. got %v", err)
	}
	if !strings.Contains(err.Error(), "field_that_cannot_exist") || !strings.Contains(err.Error(), `step "second"`) {
		t.Errorf("the refusal must name the step and the missing path so the author can find it: %v", err)
	}
}

func TestADryRunStillAcceptsAnExpectationReferencingARealField(t *testing.T) {
	rec := dryRunChain(t, "dry-good-expect-ref", []chain.Expectation{
		{Path: "error.code", Equals: "OK"},
		{Path: "id", NotEqual: "${steps.first.response.id}"},
		{Path: "error.code", Equals: "${first.error.code}"},
	})

	if !rec.Passed() {
		t.Fatalf("a reference the synthesized response can satisfy must stay clean in a dry run, or "+
			"every cross-step invariant becomes unwritable: %s", rec.Failure)
	}
}

func TestADryRunRefusesAnExpectationReadingAnExportAliasAsAResponseField(t *testing.T) {
	err := dryRunRefusal(t, "dry-alias-expect-ref", []chain.Expectation{
		{Path: "error.code", Equals: "OK"},
		{Path: "id", Equals: "${first.alias_only}"},
	})

	if err == nil || !strings.Contains(err.Error(), "alias_only") {
		t.Fatal("an export alias is not a field on the response: ${step.alias} must be refused even " +
			"though ${alias} on its own resolves, which is the shape that survived both gates and " +
			"only failed live")
	}
}

func TestADryRunRefusalNamesEachBadReferenceOnce(t *testing.T) {
	c := &chain.Chain{Name: "dry-typo", Steps: []*chain.Step{
		{ID: "first", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "second", Call: "ThingService/Fetch", Body: map[string]any{"id": "${first.idd}"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "third", Call: "ThingService/Fetch", Body: map[string]any{"id": "${first.idd}"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer()
	defer srv.Close()
	_, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{DryRun: true})
	if err == nil {
		t.Fatal("a reference to a field the producer lacks must refuse the dry run")
	}
	msg := err.Error()
	if strings.Count(msg, "${first.idd}") != 1 || !strings.Contains(msg, `step "second" (step 2), "third" (step 3): ${first.idd}`) {
		t.Fatalf("the bad reference should be listed once with every step that reads it: %s", msg)
	}
	if !strings.Contains(msg, `did you mean "id"?`) {
		t.Fatalf("the refusal should suggest the field it resembles: %s", msg)
	}
}
