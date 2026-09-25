package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAPinOnATopLevelCodeFieldPinsTheVerdict(t *testing.T) {
	defer chain.ApplyCodeFields(nil)
	chain.ApplyCodeFields([]string{"message"})
	for _, pin := range []chain.Expectation{
		{Path: "status.message", Equals: "qty must be positive"},
		{Path: "status.message", Contains: "positive"},
	} {
		srv := newShopServer(t, refusedStock())
		c := normalized(t, addStockChain(pin))
		rec, err := srv.runner().Run(context.Background(), c, runner.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Status != runner.StatusPassed {
			t.Fatalf("%+v pins a code field beside the verdict, as a refused batch line's code field does, so it pins the "+
				"refusal; got %s %+v", pin, rec.Status, rec.Steps[0].Expect)
		}
	}
	for _, weak := range []chain.Expectation{
		{Path: "status.message", NotEmpty: true},
		{Path: "status.message", NotEqual: "boom"},
		{Path: "status.message", Equals: ""},
		{Path: "qty_on_hand", Equals: 0},
	} {
		srv := newShopServer(t, refusedStock())
		c := normalized(t, addStockChain(weak))
		rec, err := srv.runner().Run(context.Background(), c, runner.Options{})
		if err != nil {
			t.Fatal(err)
		}
		envelope := false
		for _, r := range rec.Steps[0].Expect {
			envelope = envelope || (r.Rule == "envelope" && strings.Contains(r.Detail, "no expectation on this step pins the verdict"))
		}
		if rec.Status != runner.StatusFailed || !envelope {
			t.Fatalf("%+v says nothing about which code the backend answered, so it must not pin the verdict; got %s %q", weak, rec.Status, rec.Failure)
		}
	}
}
