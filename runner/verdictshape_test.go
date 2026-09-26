package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

func rawStockRunner(t *testing.T, body string) *runner.Runner {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	chain.SetEnvelope("status.code", "SUCCESS")
	t.Cleanup(func() { chain.SetEnvelope("", "") })
	return &runner.Runner{Catalog: catalogtest.Shop(), Client: transport.New(transport.Options{BaseURL: srv.URL}), ValidateInput: true}
}

func TestAStatusThatIsAStringCarriesNoVerdictAndFailsTheStep(t *testing.T) {
	r := rawStockRunner(t, `{"status":"SUCCESS","qtyOnHand":"5"}`)
	rec, err := r.Run(context.Background(), normalized(t, addStockChain(chain.Expectation{Path: "qty_on_hand", Equals: 5})), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusFailed {
		t.Fatalf("status is a string, so there is no verdict at status.code; the step must fail as an absent verdict, got %s %+v",
			rec.Status, rec.Steps[0].Expect)
	}
	if !strings.Contains(rec.Failure, "no verdict") {
		t.Fatalf("the failure must say no verdict was sent, got %q", rec.Failure)
	}
}

func TestAResponseThatRepeatsAKeyFailsTheStep(t *testing.T) {
	for _, body := range []string{
		`{"status":{"code":"REJECTED","message":"denied"},"qtyOnHand":"5","status":{"code":"SUCCESS"}}`,
		`{"status":{"code":"SUCCESS","code":"REJECTED"},"qtyOnHand":"5"}`,
	} {
		r := rawStockRunner(t, body)
		rec, err := r.Run(context.Background(), normalized(t, addStockChain(
			chain.Expectation{Path: "status.code", Equals: "SUCCESS"}, chain.Expectation{Path: "qty_on_hand", Equals: 5})), runner.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Status != runner.StatusFailed {
			t.Fatalf("%s: a decoder keeps one of two values for a repeated key, and which one is not something a verdict can rest on; "+
				"the step must fail, got %s %+v", body, rec.Status, rec.Steps[0].Expect)
		}
		if !strings.Contains(rec.Steps[0].Error, "repeats") || !strings.Contains(rec.Steps[0].Error, "status") {
			t.Fatalf("%s: the error must name the repeated key, got %q", body, rec.Steps[0].Error)
		}
	}
}
