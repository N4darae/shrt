package runner_test

import (
	"context"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func refusedWithMessage() map[string]any {
	return map[string]any{"AddStock": map[string]any{
		"status":    map[string]any{"code": "REJECTED", "message": "qty must be positive"},
		"qtyOnHand": "0",
	}}
}

func TestARuleOnASiblingOfTheVerdictDoesNotPinARefusal(t *testing.T) {
	for _, pin := range []chain.Expectation{
		{Path: "status.message", NotEqual: "boom"},
		{Path: "status.message", Equals: "qty must be positive"},
		{Path: "status.message", Contains: "positive"},
	} {
		srv := newShopServer(t, refusedWithMessage())
		rec, err := srv.runner().Run(context.Background(), normalized(t, addStockChain(pin)), runner.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Status != runner.StatusFailed {
			t.Fatalf("%+v is on a sibling of status.code, not the verdict, so the refused step must fail; got %s %+v",
				pin, rec.Status, rec.Steps[0].Expect)
		}
	}
}

func TestAnEmptyMessageRuleDoesNotPinARefusal(t *testing.T) {
	answer := map[string]any{"AddStock": map[string]any{"status": map[string]any{"code": "REJECTED"}, "qtyOnHand": "0"}}
	srv := newShopServer(t, answer)
	rec, err := srv.runner().Run(context.Background(), normalized(t, addStockChain(chain.Expectation{Path: "status.message", Equals: ""})), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusFailed {
		t.Fatalf("status.message equals \"\" holds on a success as well, so the refusal must fail the step; got %s %+v", rec.Status, rec.Steps[0].Expect)
	}
}

func TestANotEqualOkDoesNotPinAnEmptyVerdict(t *testing.T) {
	answer := map[string]any{"AddStock": map[string]any{"status": map[string]any{"code": ""}, "qtyOnHand": "5"}}
	srv := newShopServer(t, answer)
	rec, err := srv.runner().Run(context.Background(), normalized(t, addStockChain(chain.Expectation{Path: "status.code", NotEqual: "SUCCESS"})), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusFailed {
		t.Fatalf("not_equal SUCCESS holds on an empty verdict without saying what was answered, so the absent-verdict guard must fail the step; got %s %+v",
			rec.Status, rec.Steps[0].Expect)
	}
}

func TestAnEmptyVerdictPinnedAsEmptyPasses(t *testing.T) {
	answer := map[string]any{"AddStock": map[string]any{"status": map[string]any{}, "qtyOnHand": "5"}}
	srv := newShopServer(t, answer)
	rec, err := srv.runner().Run(context.Background(), normalized(t, addStockChain(chain.Expectation{Path: "status.code", Equals: ""})), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusPassed {
		t.Fatalf("status.code equals \"\" pins an empty verdict; got %s %+v", rec.Status, rec.Steps[0].Expect)
	}
}

func TestACasefoldedVerdictPathPinsTheRefusal(t *testing.T) {
	for _, pin := range []chain.Expectation{
		{Path: "Status.Code", NotEqual: "SUCCESS"},
		{Path: "Status.Code", Equals: "REJECTED"},
		{Path: "STATUS.code", Equals: "REJECTED"},
	} {
		srv := newShopServer(t, refusedWithMessage())
		rec, err := srv.runner().Run(context.Background(), normalized(t, addStockChain(pin)), runner.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Status != runner.StatusPassed {
			t.Fatalf("%+v reads status.code, so it pins the refusal; got %s %+v", pin, rec.Status, rec.Steps[0].Expect)
		}
	}
}
