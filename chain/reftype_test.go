package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func addStockFrom(qty string) *chain.Chain {
	ok := []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}
	c := &chain.Chain{Name: "reftype", Steps: []*chain.Step{
		{ID: "cp", Call: "shop.catalog.v1.ProductService/CreateProduct",
			Body:   map[string]any{"sku": "s", "name": "n", "price_minor": "250"},
			Expect: ok, Export: map[string]string{"pid": "product.id_product", "price": "product.price_minor"}},
		{ID: "add", Call: "shop.catalog.v1.StockService/AddStock",
			Body: map[string]any{"id_product": "${cp.product.id_product}", "qty": qty}, Expect: ok},
	}}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func TestAReferenceWhoseTypeCannotFillANumericFieldIsRefusedUpFront(t *testing.T) {
	cat := catalogtest.Shop()
	for _, ref := range []string{"${pid}", "${exports.pid}", "${cp.product.sku}", "${steps.cp.request.name}", "${cp.product}"} {
		c := addStockFrom(ref)
		lintErr := false
		for _, i := range chain.Lint(c, cat) {
			if i.IsError() && strings.Contains(i.Message, ref) && strings.Contains(i.Message, "int64") {
				lintErr = true
			}
		}
		if !lintErr {
			t.Errorf("qty: %q fills an int64 from a non-numeric field and lint did not error: %+v", ref, chain.Lint(c, cat))
		}
		if p := c.ResponseRefProblems(cat); len(p) == 0 || !strings.Contains(strings.Join(p, " "), ref) {
			t.Errorf("qty: %q must be refused before anything is sent, got %v", ref, p)
		}
	}
}

func TestANumericReferenceIntoANumericFieldIsNotReported(t *testing.T) {
	cat := catalogtest.Shop()
	for _, ref := range []string{"${price}", "${cp.product.price_minor}", "${steps.cp.request.price_minor}", "5", "${vars.qty}"} {
		c := addStockFrom(ref)
		if ref == "${vars.qty}" {
			c.Vars = map[string]any{"qty": "5"}
		}
		if p := c.ResponseRefProblems(cat); len(p) != 0 {
			t.Errorf("qty: %q is numeric, got %v", ref, p)
		}
		for _, i := range chain.Lint(c, cat) {
			if i.IsError() && strings.Contains(i.Message, "int64") {
				t.Errorf("qty: %q is numeric, got %+v", ref, i)
			}
		}
	}
}
