package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func refusedStock() map[string]any {
	return map[string]any{"AddStock": map[string]any{
		"status":    map[string]any{"code": "REJECTED", "message": "qty must be positive"},
		"qtyOnHand": "0",
	}}
}

func TestAnInBandRefusalNoExpectationPinsFailsTheStep(t *testing.T) {
	srv := newShopServer(t, refusedStock())
	c := normalized(t, addStockChain(chain.Expectation{Path: "qty_on_hand", Equals: 0}))
	rec, err := srv.runner().Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusFailed {
		t.Fatalf("the backend refused the call in-band and nothing on the step says a refusal is expected; "+
			"the data assertion held only on the zero value the refusal left, so the step must not pass; got %s %+v",
			rec.Status, rec.Steps[0].Expect)
	}
	if !strings.Contains(rec.Failure, "refused in-band") || !strings.Contains(rec.Failure, "REJECTED") {
		t.Fatalf("the failure should say the call was refused in-band and with what, got %q", rec.Failure)
	}
}

func TestAnInBandRefusalThatIsPinnedStillPasses(t *testing.T) {
	for _, pin := range []chain.Expectation{
		{Path: "status.code", Equals: "REJECTED"},
		{Path: "status.code", NotEqual: "SUCCESS"},
		{Path: "status.message", Contains: "positive"},
	} {
		srv := newShopServer(t, refusedStock())
		c := normalized(t, addStockChain(pin, chain.Expectation{Path: "qty_on_hand", Equals: 0}))
		rec, err := srv.runner().Run(context.Background(), c, runner.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Status != runner.StatusPassed {
			t.Fatalf("%+v pins the refusal, so the step asserted what happened and passes; got %s %+v", pin, rec.Status, rec.Steps[0].Expect)
		}
	}
}
