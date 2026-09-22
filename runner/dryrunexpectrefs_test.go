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

func TestADryRunRefusesAnExpectationReferencingAFieldNoResponseCarries(t *testing.T) {
	rec := dryRunChain(t, "dry-bad-expect-ref", []chain.Expectation{
		{Path: "error.code", Equals: "OK"},
		{Path: "id", Equals: "${first.field_that_cannot_exist}"},
	})

	if rec.Passed() {
		t.Fatalf("a dry run must refuse a reference no response can satisfy; the live run is not the "+
			"first place to learn this. record=%s", rec.Status)
	}
	second, ok := rec.Step("second")
	if !ok {
		t.Fatal("no record for step second")
	}
	var unresolved *chain.ExpectResult
	for i := range second.Expect {
		if second.Expect[i].Rule == "unresolved" {
			unresolved = &second.Expect[i]
		}
	}
	if unresolved == nil {
		t.Fatalf("the refusal must name the expectation that could not resolve, not just fail the "+
			"step: expect=%+v", second.Expect)
	}
	if !strings.Contains(unresolved.Detail, "field_that_cannot_exist") {
		t.Errorf("the detail must name the missing path so the author can find it: %q", unresolved.Detail)
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
	rec := dryRunChain(t, "dry-alias-expect-ref", []chain.Expectation{
		{Path: "error.code", Equals: "OK"},
		{Path: "id", Equals: "${first.alias_only}"},
	})

	if rec.Passed() {
		t.Fatal("an export alias is not a field on the response: ${step.alias} must be refused even " +
			"though ${alias} on its own resolves, which is the shape that survived both gates and " +
			"only failed live")
	}
}
