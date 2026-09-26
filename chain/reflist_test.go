package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func orderThen(next *chain.Step) *chain.Chain {
	ok := []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}
	c := &chain.Chain{Name: "reflist", Steps: []*chain.Step{
		{ID: "cp", Call: "shop.catalog.v1.ProductService/CreateProduct",
			Body: map[string]any{"sku": "s", "name": "n", "price_minor": "250"}, Expect: ok},
		{ID: "o", Call: "shop.orders.v1.OrderService/CreateOrder",
			Body:   map[string]any{"id_customer": "c", "lines": []any{map[string]any{"id_product": "${cp.product.id_product}", "qty": 1}}},
			Expect: ok, Export: map[string]string{"lines": "order.lines", "first_qty": "order.lines.0.qty"}},
		next,
	}}
	c.Steps[2].Expect = ok
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func refusedUpFront(t *testing.T, c *chain.Chain, ref string) {
	t.Helper()
	cat := catalogtest.Shop()
	lintErr := false
	for _, i := range chain.Lint(c, cat) {
		if i.IsError() && strings.Contains(i.Message, ref) {
			lintErr = true
		}
	}
	if !lintErr {
		t.Errorf("%s: lint must error, got %+v", ref, chain.Lint(c, cat))
	}
	if p := c.ResponseRefProblems(cat); len(p) == 0 || !strings.Contains(strings.Join(p, " "), ref) {
		t.Errorf("%s: must be refused before anything is sent, got %v", ref, p)
	}
}

func notRefused(t *testing.T, c *chain.Chain, ref string) {
	t.Helper()
	cat := catalogtest.Shop()
	if p := c.ResponseRefProblems(cat); len(p) != 0 {
		t.Errorf("%s: must not be refused, got %v", ref, p)
	}
	for _, i := range chain.Lint(c, cat) {
		if i.IsError() && strings.Contains(i.Message, ref) {
			t.Errorf("%s: must not be a lint error, got %+v", ref, i)
		}
	}
}

func TestAListReferenceIntoAScalarFieldIsRefusedUpFront(t *testing.T) {
	for _, ref := range []string{"${o.order.lines}", "${steps.o.request.lines}", "${lines}"} {
		refusedUpFront(t, orderThen(&chain.Step{ID: "x", Call: "shop.catalog.v1.StockService/AddStock",
			Body: map[string]any{"id_product": "${cp.product.id_product}", "qty": ref}}), ref)
		refusedUpFront(t, orderThen(&chain.Step{ID: "x", Call: "shop.catalog.v1.StockService/AddStock",
			Body: map[string]any{"id_product": ref, "qty": 1}}), ref)
	}
}

func TestAScalarReferenceIntoARepeatedFieldIsRefusedUpFront(t *testing.T) {
	for _, ref := range []string{"${o.order.id_order}", "${first_qty}"} {
		refusedUpFront(t, orderThen(&chain.Step{ID: "x", Call: "shop.orders.v1.OrderService/CreateOrder",
			Body: map[string]any{"id_customer": "c", "lines": ref}}), ref)
	}
}

func TestAListReferenceIntoARepeatedFieldOrAnElementIntoAScalarIsNotRefused(t *testing.T) {
	notRefused(t, orderThen(&chain.Step{ID: "x", Call: "shop.orders.v1.OrderService/CreateOrder",
		Body: map[string]any{"id_customer": "c", "lines": "${o.order.lines}"}}), "${o.order.lines}")
	notRefused(t, orderThen(&chain.Step{ID: "x", Call: "shop.catalog.v1.StockService/AddStock",
		Body: map[string]any{"id_product": "${cp.product.id_product}", "qty": "${o.order.lines.0.qty}"}}), "${o.order.lines.0.qty}")
}
