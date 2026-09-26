package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func tagChain(sku string) *chain.Chain {
	c := &chain.Chain{Name: "wholetag", Vars: map[string]any{"tag": "t1"}, Steps: []*chain.Step{
		{ID: "cp", Call: "shop.catalog.v1.ProductService/CreateProduct",
			Body:   map[string]any{"sku": sku, "name": "n", "price_minor": "250"},
			Expect: []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}, {Path: "product.price_minor", Equals: 250}}},
	}}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func TestAWholeValueTagInAStringFieldIsWarned(t *testing.T) {
	cat := catalogtest.Shop()
	found := false
	for _, i := range chain.Lint(tagChain("${vars.tag}"), cat) {
		if strings.Contains(i.Message, "${vars.tag}") && strings.Contains(i.Message, "sku-${vars.tag}") {
			if i.IsError() {
				t.Fatalf("a whole-value tag is a warning, not an error: %+v", i)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("sku: ${vars.tag} is input, so a CI gate's fresh -var tag makes every verify drift; lint must warn: %+v", chain.Lint(tagChain("${vars.tag}"), cat))
	}
	for _, i := range chain.Lint(tagChain("sku-${vars.tag}"), cat) {
		if strings.Contains(i.Message, "whole value") {
			t.Fatalf("a tag inside other text is a fixture name and must not be warned: %+v", i)
		}
	}
}
