package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAPinTheRefusalItselfSatisfiesDoesNotDeclareIt(t *testing.T) {
	for _, pin := range []chain.Expectation{
		{Path: "status.code", NotEqual: ""},
		{Path: "status.code", NotEqual: "REJECTD"},
		{Path: "status.code", NotEqual: "${vars.typo}"},
		{Path: "transport.code", Equals: "ok"},
	} {
		srv := newShopServer(t, refusedStock())
		c := addStockChain(pin, chain.Expectation{Path: "qty_on_hand", Equals: 0})
		c.Vars = map[string]any{"typo": "REJECTD"}
		c = normalized(t, c)
		rec, err := srv.runner().Run(context.Background(), c, runner.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Status != runner.StatusFailed || !strings.Contains(rec.Failure, "refused in-band") {
			t.Errorf("%+v holds on SUCCESS and on the REJECTED answer alike, so it says nothing about the refusal; "+
				"the step must fail as refused in-band, got %s %q %+v", pin, rec.Status, rec.Failure, rec.Steps[0].Expect)
		}
	}
}

func TestAPinThatFailsOnTheOkValueStillDeclaresTheRefusal(t *testing.T) {
	for _, pin := range []chain.Expectation{
		{Path: "status.code", NotEqual: "SUCCESS"},
		{Path: "status.code", NotEqual: "${vars.ok}"},
		{Path: "status.code", Equals: "REJECTED"},
		{Path: "status.code", Contains: "REJ"},
	} {
		srv := newShopServer(t, refusedStock())
		c := addStockChain(pin, chain.Expectation{Path: "qty_on_hand", Equals: 0})
		c.Vars = map[string]any{"ok": "SUCCESS"}
		c = normalized(t, c)
		rec, err := srv.runner().Run(context.Background(), c, runner.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Status != runner.StatusPassed {
			t.Errorf("%+v would fail on SUCCESS, so it declares the refusal; got %s %+v", pin, rec.Status, rec.Steps[0].Expect)
		}
	}
}

func TestAnItemLineIsNotDeclaredByAPinItsRefusalSatisfies(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	for pin, declared := range map[string]bool{"": false, "INVALID_ARGUMNT": false, "${vars.typo}": false, "${vars.ok}": true, "OK": true} {
		srv := newFakeServer()
		c := batchChain("BatchService/Preview", []any{"ok", "bad"},
			chain.Expectation{Path: "error.code", Equals: "OK"},
			chain.Expectation{Path: "results.1.error.code", NotEqual: pin})
		c.Vars = map[string]any{"typo": "INVALID_ARGUMNT", "ok": "OK"}
		rec, err := newBatchRunner(t, srv).Run(context.Background(), c, runner.Options{})
		srv.Close()
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		_, surprised := itemEnvelopeResult(t, rec)
		if surprised == declared {
			t.Errorf("results.1.error.code not_equal %q: declared=%v, but item_envelope fired=%v: %+v", pin, declared, surprised, rec.Steps[0].Expect)
		}
	}
}
