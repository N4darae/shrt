package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAnItemVerdictUnderACaseVariantKeyFailsTheStep(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	srv := newFakeServer()
	defer srv.Close()
	srv.itemKey = "Error"
	r := newBatchRunner(t, srv)
	r.ValidateOutput = false

	c := batchChain("BatchService/Preview", []any{"ok", "ok"}, chain.Expectation{Path: "error.code", Equals: "OK"})
	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatalf("every line's verdict sits under Error, which protojson does not read, so no line says it succeeded: %+v", rec.Steps[0].Expect)
	}
	res, found := itemEnvelopeResult(t, rec)
	if !found || !strings.Contains(res.Got.(string), "results.0.error.code = "+chain.NoItemVerdict) || !strings.Contains(res.Got.(string), "results.1.error.code") {
		t.Fatalf("each line must be reported as carrying no verdict: %+v", rec.Steps[0].Expect)
	}
	if !strings.Contains(rec.Steps[0].Warning, "results.0 carries its verdict under Error") {
		t.Errorf("the warning must name the case-variant key: %q", rec.Steps[0].Warning)
	}
}
