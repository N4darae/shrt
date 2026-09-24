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

func refusingSecondRunner(t *testing.T, status int, contentType, refusal string) *runner.Runner {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			w.Header().Set("Content-Type", contentType)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(refusal))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`))
	}))
	t.Cleanup(srv.Close)
	chain.SetEnvelope("status.code", "SUCCESS")
	t.Cleanup(func() { chain.SetEnvelope("", "") })
	return &runner.Runner{Catalog: catalogtest.Shop(), Client: transport.New(transport.Options{BaseURL: srv.URL}), ValidateInput: true}
}

func TestATransportRefusalAtAPinnedStepIsNotAsPinned(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"internal", "application/json", `{"code":"internal","message":"panic: nil map"}`, 500},
		{"not found", "application/json", `{"code":"not_found","message":"no such rpc"}`, 404},
		{"permission denied", "application/json", `{"code":"permission_denied","message":"nope"}`, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := refusingSecondRunner(t, tc.status, tc.contentType, tc.body)
			c := keptRedChain(5, 5, chain.Pin{Step: "second", Path: "qty_on_hand"})
			c.Steps[1].Expect = []chain.Expectation{{Path: "qty_on_hand", Equals: 5}}
			rec, err := r.Run(context.Background(), normalized(t, c), runner.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if rec.KeptRed == runner.KeptRedAsPinned {
				t.Fatalf("an unevaluated expectation never satisfies a pin: got as_pinned (note %q)", rec.KeptRedNote)
			}
			if rec.KeptRed != runner.KeptRedNotAsPinned || !strings.Contains(rec.KeptRedNote, `step "second": the pinned step was refused at transport`) {
				t.Fatalf("want not_as_pinned naming the transport refusal, got %q: %q", rec.KeptRed, rec.KeptRedNote)
			}
		})
	}
}

func TestAGatewayAnswerAtAPinnedStepIsAnErrorRunNeverAsPinned(t *testing.T) {
	r := refusingSecondRunner(t, 503, "text/html", `<html>bad gateway</html>`)
	c := keptRedChain(5, 5, chain.Pin{Step: "second", Path: "qty_on_hand"})
	c.Steps[1].Expect = []chain.Expectation{{Path: "qty_on_hand", Equals: 5}}
	rec, err := r.Run(context.Background(), normalized(t, c), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.KeptRed == runner.KeptRedAsPinned {
		t.Fatalf("a gateway answer never satisfies a pin: got as_pinned (note %q)", rec.KeptRedNote)
	}
	if rec.Status != runner.StatusError {
		t.Fatalf("a bare 503 was not answered by the service, so the run is an error, got %q", rec.Status)
	}
}

func TestAnUnevaluatedPinnedExpectationIsNotAsPinned(t *testing.T) {
	r := rawStockRunner(t, `{"status":{"code":"SUCCESS"},"qtyOnHand":"5","qtyOnHand":"5"}`)
	c := keptRedChain(5, 5, chain.Pin{Step: "second", Path: "qty_on_hand"})
	c.Steps = c.Steps[1:]
	c.KeptRed[0].Step = "second"
	rec, err := r.Run(context.Background(), normalized(t, c), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.KeptRed != runner.KeptRedNotAsPinned || !strings.Contains(rec.KeptRedNote, "was not evaluated") {
		t.Fatalf("a pinned expectation that never ran is not as pinned, got %q: %q", rec.KeptRed, rec.KeptRedNote)
	}
}
