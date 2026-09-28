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

func TestAnExportNamedLikeAStepIsRefusedBeforeAnythingIsSent(t *testing.T) {
	var sent atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":{"code":"SUCCESS"},"product":{"idProduct":"p1"}}`))
	}))
	defer srv.Close()
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	r := &runner.Runner{Catalog: catalogtest.Shop(), Client: transport.New(transport.Options{BaseURL: srv.URL}), ValidateInput: true}
	build := func() *chain.Chain {
		return normalized(t, &chain.Chain{Name: "clash", Steps: []*chain.Step{
			{ID: "made", Call: "shop.catalog.v1.ProductService/CreateProduct", Body: map[string]any{"sku": "a"},
				Export: map[string]string{"made": "product.id_product"}},
			{ID: "read", Call: "shop.catalog.v1.ProductService/GetProduct", Body: map[string]any{"id_product": "${made}"}},
		}})
	}
	for _, dry := range []bool{false, true} {
		_, err := r.Run(context.Background(), build(), runner.Options{DryRun: dry})
		if err == nil || !strings.Contains(err.Error(), `export "made" has the same name as step "made"`) || !strings.Contains(err.Error(), "nothing was sent") {
			t.Fatalf("dry=%v: run must refuse the ambiguous name with lint's reason; got %v", dry, err)
		}
	}
	if sent.Load() != 0 {
		t.Fatalf("nothing may be sent, got %d requests", sent.Load())
	}
}
