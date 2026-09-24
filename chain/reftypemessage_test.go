package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func createProductNamed(name string) *chain.Chain {
	ok := []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}
	c := &chain.Chain{Name: "reftypemsg", Steps: []*chain.Step{
		{ID: "cp", Call: "shop.catalog.v1.ProductService/CreateProduct",
			Body:   map[string]any{"sku": "s", "name": "n", "price_minor": "250"},
			Expect: ok, Export: map[string]string{"prod": "product", "pid": "product.id_product"}},
		{ID: "cp2", Call: "shop.catalog.v1.ProductService/CreateProduct",
			Body: map[string]any{"sku": "s2", "name": name, "price_minor": "250"}, Expect: ok},
	}}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func placeOrderWith(field, ref string) *chain.Chain {
	c := &chain.Chain{Name: "reftypewkt", Steps: []*chain.Step{
		{ID: "p1", Call: "shrt.test.rich.v1.OrderService/PlaceOrder",
			Body: map[string]any{"id_order": "o1", "due_at": "2026-01-01T00:00:00Z", "flagged": true,
				"first_line": map[string]any{"sku": "a", "qty": "1"}}},
		{ID: "p2", Call: "shrt.test.rich.v1.OrderService/PlaceOrder",
			Body: map[string]any{field: ref}},
	}}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func lintErrorNaming(c *chain.Chain, ref string) bool {
	for _, i := range chain.Lint(c, catalogtest.Shop()) {
		if i.IsError() && strings.Contains(i.Message, ref) {
			return true
		}
	}
	return false
}

func TestAMessageReferenceIntoAStringFieldIsRefusedUpFront(t *testing.T) {
	cat := catalogtest.Shop()
	for _, ref := range []string{"${cp.product}", "${prod}", "${exports.prod}", "${cp.status}"} {
		c := createProductNamed(ref)
		if !lintErrorNaming(c, ref) {
			t.Errorf("name: %q fills a string from a message and lint did not error: %+v", ref, chain.Lint(c, cat))
		}
		if p := c.ResponseRefProblems(cat); len(p) == 0 || !strings.Contains(strings.Join(p, " "), ref) {
			t.Errorf("name: %q must be refused before anything is sent, got %v", ref, p)
		}
	}
	for _, ref := range []string{"${pid}", "${cp.product.sku}", "${cp.status.code}"} {
		if p := createProductNamed(ref).ResponseRefProblems(cat); len(p) != 0 {
			t.Errorf("name: %q reads a string, got %v", ref, p)
		}
	}
}

func TestAMessageReferenceIntoScalarFieldsOfRichTypes(t *testing.T) {
	cat := catalogtest.Rich()
	refused := [][2]string{
		{"id_order", "${steps.p1.request.first_line}"},
		{"channel", "${steps.p1.request.first_line}"},
		{"memo", "${steps.p1.request.first_line}"},
	}
	for _, tc := range refused {
		if p := placeOrderWith(tc[0], tc[1]).ResponseRefProblems(cat); len(p) == 0 {
			t.Errorf("%s: %s is a message and cannot fill it, but nothing was refused", tc[0], tc[1])
		}
	}
	allowed := [][2]string{
		{"id_order", "${steps.p1.request.due_at}"},
		{"memo", "${steps.p1.request.flagged}"},
		{"id_order", "${steps.p1.request.first_line.sku}"},
		{"first_line", "${steps.p1.request.first_line}"},
	}
	for _, tc := range allowed {
		if p := placeOrderWith(tc[0], tc[1]).ResponseRefProblems(cat); len(p) != 0 {
			t.Errorf("%s: %s renders as a value that field accepts, got %v", tc[0], tc[1], p)
		}
	}
}
