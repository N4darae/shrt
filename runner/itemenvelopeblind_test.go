package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func newLenientBatchRunner(t *testing.T, srv *fakeServer) *runner.Runner {
	t.Helper()
	deps, err := runner.Build(context.Background(), testConfig(srv.URL), catalogtest.Batch(), nil)
	if err != nil {
		t.Fatalf("build deps: %v", err)
	}
	return &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, ValidateOutput: false, Auth: deps.Bindings}
}

func TestAnUnsetPerItemEnvelopeIsSuccessNotAMisconfiguration(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	srv := newFakeServer()
	defer srv.Close()
	srv.batchUnset = true

	c := batchChain("BatchService/Preview", []any{"ok", "fine"}, chain.Expectation{Path: "error.code", Equals: "OK"})
	rec, err := newBatchRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("proto3 renders an unset per-item envelope as null, so a batch where every line succeeded "+
			"carries no verdict to read; calling that a misconfiguration fails a healthy run: %s %s\n%+v",
			rec.Status, rec.Failure, rec.Steps[0].Expect)
	}
}

func TestAStaleDescriptorDoesNotSilenceThePerItemCheck(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	srv := newFakeServer()
	defer srv.Close()
	srv.receiptDrift = true

	c := batchChain("BatchService/Receipt", []any{"ok", "bad"}, chain.Expectation{Path: "error.code", Equals: "OK"})
	rec, err := newLenientBatchRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Steps[0].Warning == "" {
		t.Fatal("this response does not match the descriptor, so the run must say the descriptor is stale")
	}
	if rec.Passed() {
		t.Fatal("a response the descriptor no longer matches is kept raw, and the per-item gate then read " +
			"that same stale descriptor and skipped itself — every line of a batch could be refused behind " +
			"an OK envelope with nothing but a warning to show for it")
	}
	res, found := itemEnvelopeResult(t, rec)
	if !found || !strings.Contains(res.Got.(string), "results.1.error.code = invalid_argument") {
		t.Errorf("the refused line must still be named, got %+v", rec.Steps[0].Expect)
	}
}

func TestANonBatchListUnderAStaleDescriptorWarnsRatherThanFails(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	srv := newFakeServer()
	defer srv.Close()

	c := batchChain("BatchService/Receipt", []any{"a"}, chain.Expectation{Path: "error.code", Equals: "OK"})
	rec, err := newLenientBatchRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("this rpc's results[] carries no verdict field at all, which the descriptor states plainly; "+
			"it must not be reported as a refusal: %s %s", rec.Status, rec.Failure)
	}
}
