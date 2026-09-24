package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestARunRefusedAsAWholeDoesNotBlameEnvelopeOK(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.createRefusal = "REJECTED"

	c := normalized(t, &chain.Chain{Name: "refused", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Warning != "" {
		t.Fatalf("REJECTED is a refusal, not another spelling of success: the principal was refused, "+
			"the config is not wrong, got %q", rec.Warning)
	}
}

func TestAStepAssertingATransportRefusalDoesNotBlameEnvelopeOK(t *testing.T) {
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
	if rec.Warning != "" {
		t.Fatalf("a step that asserts a transport refusal expected no success, so its envelope says "+
			"nothing about envelope_ok, got %q", rec.Warning)
	}
}

func TestAnEnvelopePathOnAFreeTextFieldBlamesEnvelopePathAndShowsEmptyValues(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("error.message", "OK")
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(rec.Warning, "conventions.envelope_path") {
		t.Fatalf("an empty message is not a verdict, so the path is what is wrong, got %q", rec.Warning)
	}
	if !strings.Contains(rec.Warning, `"" (2)`) {
		t.Fatalf("an empty value must be printed visibly, got %q", rec.Warning)
	}
}
