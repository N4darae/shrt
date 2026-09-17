package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func newBatchRunner(t *testing.T, srv *fakeServer) *runner.Runner {
	t.Helper()
	deps, err := runner.Build(context.Background(), testConfig(srv.URL), catalogtest.Batch(), nil)
	if err != nil {
		t.Fatalf("build deps: %v", err)
	}
	return &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, ValidateOutput: true, Auth: deps.Bindings}
}

func batchChain(call string, lines []any, expect ...chain.Expectation) *chain.Chain {
	c := &chain.Chain{
		Name: "batch-flow",
		Steps: []*chain.Step{{
			ID:     "batch",
			Call:   call,
			Body:   map[string]any{"lines": lines},
			Expect: expect,
		}},
	}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func itemEnvelopeResult(t *testing.T, rec *runner.Record) (chain.ExpectResult, bool) {
	t.Helper()
	if len(rec.Steps) == 0 {
		t.Fatal("no step record")
	}
	for _, e := range rec.Steps[0].Expect {
		if e.Rule == "item_envelope" {
			return e, true
		}
	}
	return chain.ExpectResult{}, false
}

func TestItemEnvelopeFailsAStepWhoseBatchWasRefusedBehindAnOKEnvelope(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	srv := newFakeServer()
	defer srv.Close()

	c := batchChain("BatchService/Preview", []any{"ok", "bad"}, chain.Expectation{Path: "error.code", Equals: "OK"})
	rec, err := newBatchRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("the envelope said OK while line 1 was refused, and a step asserting only the envelope passed — the check the convention exists for did not fire")
	}
	res, found := itemEnvelopeResult(t, rec)
	if !found {
		t.Fatalf("no item_envelope result on the step, so the record does not say WHY it failed: %+v", rec.Steps[0].Expect)
	}
	if !strings.Contains(res.Got.(string), "results.1.error.code = invalid_argument") {
		t.Errorf("got = %q, want it to name the refused line and its code", res.Got)
	}
	if rec.Steps[0].Error != "" {
		t.Errorf("an assertion failure must not be reported as a step error, got %q", rec.Steps[0].Error)
	}
}

func TestARefusalTheStepAssertsIsADeclaredOutcomeNotASurprise(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	srv := newFakeServer()
	defer srv.Close()

	c := batchChain("BatchService/Preview", []any{"ok", "bad"},
		chain.Expectation{Path: "error.code", Equals: "OK"},
		chain.Expectation{Path: "results.0.error.code", Equals: "OK"},
		chain.Expectation{Path: "results.1.error.code", Equals: "invalid_argument"},
	)
	rec, err := newBatchRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("a negative test that pins the refused line's own verdict must pass, got %s: %s\n%+v",
			rec.Status, rec.Failure, rec.Steps[0].Expect)
	}
	if _, found := itemEnvelopeResult(t, rec); found {
		t.Error("no item_envelope result should be appended when every refusal was declared")
	}
}

func TestOnlyTheUndeclaredRefusalsAreReported(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	srv := newFakeServer()
	defer srv.Close()

	c := batchChain("BatchService/Preview", []any{"bad-one", "bad-two"},
		chain.Expectation{Path: "error.code", Equals: "OK"},
		chain.Expectation{Path: "results[0].error.code", NotEqual: "OK"},
	)
	rec, err := newBatchRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("line 1 was refused and nothing in the step said so — declaring line 0 must not excuse line 1")
	}
	res, found := itemEnvelopeResult(t, rec)
	if !found {
		t.Fatalf("no item_envelope result: %+v", rec.Steps[0].Expect)
	}
	got := res.Got.(string)
	if strings.Contains(got, "results.0.") {
		t.Errorf("line 0 was declared with not_equal (bracket syntax) and must not be reported, got %q", got)
	}
	if !strings.Contains(got, "results.1.error.code = invalid_argument") {
		t.Errorf("line 1 was not declared and must be reported, got %q", got)
	}
}

func TestExistsOnTheVerdictPathDeclaresNothing(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	srv := newFakeServer()
	defer srv.Close()

	yes := true
	c := batchChain("BatchService/Preview", []any{"bad"},
		chain.Expectation{Path: "error.code", Equals: "OK"},
		chain.Expectation{Path: "results.0.error.code", Exists: &yes},
	)
	rec, err := newBatchRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("exists: true on a verdict path is true for OK and for a refusal alike, so it declares no outcome and must not silence the check")
	}
}

func TestAListWhoseItemsCarryNoVerdictFieldIsNotABatchForThisConvention(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	srv := newFakeServer()
	defer srv.Close()

	c := batchChain("BatchService/Receipt", []any{"a", "b"}, chain.Expectation{Path: "error.code", Equals: "OK"})
	rec, err := newBatchRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("an atomic receipt list whose item type has no verdict field is not covered by the convention, and the "+
			"schema says so — the runner must not call it a misconfiguration. got %s: %s\n%s",
			rec.Status, rec.Failure, rec.Steps[0].Error)
	}
}

func TestNewFromConfigRefusesAnItemEnvelopeNoResponseDeclares(t *testing.T) {
	defer chain.SetItemEnvelope("")
	cfg := testConfig("http://127.0.0.1:0")
	cfg.Conventions.ItemEnvelopePath = "results[].error.code"

	if _, _, err := runner.NewFromConfig(context.Background(), cfg, catalogtest.New()); err == nil {
		t.Fatal("no response message in this descriptor has a results list, so the convention checks nothing and must be refused up front rather than silently never firing")
	} else if !strings.Contains(err.Error(), "results[].error.code") {
		t.Errorf("the error must name the path so it can be fixed, got: %v", err)
	}

	if _, _, err := runner.NewFromConfig(context.Background(), cfg, catalogtest.Batch()); err != nil {
		t.Fatalf("the batch descriptor declares results[].error.code on PreviewResponse, so the same config must be accepted: %v", err)
	}

	cfg.Conventions.ItemEnvelopePath = "results.error.code"
	_, _, err := runner.NewFromConfig(context.Background(), cfg, catalogtest.Batch())
	if err == nil || !strings.Contains(err.Error(), "<list>[].<path>") {
		t.Fatalf("a path without the [] marker cannot check anything and must be refused with the grammar spelled out, got: %v", err)
	}
}
