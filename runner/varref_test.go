package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

func TestAVarCarryingAReferenceIsRefusedBeforeAnythingIsSent(t *testing.T) {
	var sent atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":{"code":"SUCCESS"},"qtyOnHand":"1"}`))
	}))
	defer srv.Close()
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	r := &runner.Runner{Catalog: catalogtest.Shop(), Client: transport.New(transport.Options{BaseURL: srv.URL}), ValidateInput: true}
	build := func() *chain.Chain {
		return normalized(t, &chain.Chain{Name: "idem", Vars: map[string]any{"idem": "idem-${vars.tag}"}, Steps: []*chain.Step{
			{ID: "add", Call: "shop.catalog.v1.StockService/AddStock", Body: map[string]any{"id_product": "${vars.idem}", "qty": "1"},
				Expect: []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}},
		}})
	}
	for _, dry := range []bool{false, true} {
		_, err := r.Run(context.Background(), build(), runner.Options{DryRun: dry})
		if err == nil || !strings.Contains(err.Error(), `var "idem" carries ${vars.tag}`) || !strings.Contains(err.Error(), "nothing was sent") {
			t.Fatalf("dry=%v: a var value is never resolved, so run must refuse with lint's reason; got %v", dry, err)
		}
	}
	if sent.Load() != 0 {
		t.Fatalf("nothing may be sent, got %d requests", sent.Load())
	}
	rec, err := r.Run(context.Background(), build(), runner.Options{Vars: map[string]any{"idem": "idem-1"}})
	if err != nil || rec.Status != runner.StatusPassed {
		t.Fatalf("a -var that replaces the value leaves nothing unresolved; got %v %+v", err, rec)
	}
}
