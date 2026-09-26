package runner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func stockAnswer(qty any) map[string]any {
	return map[string]any{"AddStock": map[string]any{"status": map[string]any{"code": "SUCCESS"}, "qtyOnHand": qty}}
}

func addStockChain(expect ...chain.Expectation) *chain.Chain {
	return &chain.Chain{Name: "zero", Steps: []*chain.Step{{
		ID: "stock", Call: "shop.catalog.v1.StockService/AddStock",
		Body:   map[string]any{"id_product": "p-1", "qty": "0"},
		Expect: expect,
	}}}
}

func TestNotEmptyFailsOnAnInt64Zero(t *testing.T) {
	for _, wire := range []any{"0", 0, nil} {
		srv := newShopServer(t, stockAnswer(wire))
		for _, e := range []chain.Expectation{
			{Path: "qty_on_hand", NotEmpty: true},
			{Path: "qty_on_hand", NotEqual: ""},
		} {
			c := normalized(t, addStockChain(e))
			rec, err := srv.runner().Run(context.Background(), c, runner.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if rec.Status != runner.StatusFailed {
				t.Fatalf("an int64 at 0 (sent as %#v) is the zero value exactly as an int32 0 is, so %+v must fail; got %s %+v",
					wire, e, rec.Status, rec.Steps[0].Expect)
			}
		}
	}
}

func TestNotEmptyStillPassesOnANonZeroInt64(t *testing.T) {
	srv := newShopServer(t, stockAnswer("7"))
	c := normalized(t, addStockChain(chain.Expectation{Path: "qty_on_hand", NotEmpty: true},
		chain.Expectation{Path: "qty_on_hand", NotEqual: ""}))
	rec, err := srv.runner().Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusPassed {
		t.Fatalf("qty_on_hand 7 is not empty, got %s %+v", rec.Status, rec.Steps[0].Expect)
	}
}

func TestAnInt64ZeroAtARedactedPathIsNotRedacted(t *testing.T) {
	srv := newShopServer(t, stockAnswer("0"))
	c := normalized(t, addStockChain(chain.Expectation{Path: "status.code", Equals: "SUCCESS"}))
	rec, err := srv.runner().Run(context.Background(), c, runner.Options{Redact: []string{"**.qty_on_hand", "**.qty"}})
	if err != nil {
		t.Fatal(err)
	}
	var resp, req map[string]any
	if err := json.Unmarshal(rec.Steps[0].Response, &resp); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rec.Steps[0].Request, &req); err != nil {
		t.Fatal(err)
	}
	if resp["qty_on_hand"] != "0" || req["qty"] != "0" {
		t.Fatalf("an empty value, 0 included, is never redacted, whatever its proto type; got response %s request %s",
			rec.Steps[0].Response, rec.Steps[0].Request)
	}
	if strings.Contains(string(rec.Steps[0].Response), "<redacted>") {
		t.Fatalf("nothing in this response is a non-empty redacted value: %s", rec.Steps[0].Response)
	}
}
