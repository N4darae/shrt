package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAResponseThatSpellsOneFieldTwiceFailsTheStep(t *testing.T) {
	for _, body := range []string{
		`{"status":{"code":"SUCCESS"},"qtyOnHand":"999","qty_on_hand":"5"}`,
		`{"status":{"code":"SUCCESS"},"qty_on_hand":"5","qtyOnHand":"999"}`,
	} {
		r := rawStockRunner(t, body)
		rec, err := r.Run(context.Background(), normalized(t, addStockChain(
			chain.Expectation{Path: "status.code", Equals: "SUCCESS"}, chain.Expectation{Path: "qty_on_hand", Equals: 5})), runner.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Status != runner.StatusFailed {
			t.Fatalf("%s: the json name and the proto name are one field, so this is a repeated key and the step must fail, got %s %+v",
				body, rec.Status, rec.Steps[0].Expect)
		}
		if !strings.Contains(rec.Steps[0].Error, "repeats") || !strings.Contains(rec.Steps[0].Error, "qty") {
			t.Fatalf("%s: the error must name the repeated field, got %q", body, rec.Steps[0].Error)
		}
	}
	r := rawStockRunner(t, `{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`)
	rec, err := r.Run(context.Background(), normalized(t, addStockChain(chain.Expectation{Path: "qty_on_hand", Equals: 5})), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusPassed {
		t.Fatalf("one spelling of each field must still pass, got %s %s", rec.Status, rec.Failure)
	}
}
