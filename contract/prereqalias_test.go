package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestAPrerequisiteDeclaredUnderAnAliasIsScopedToThatAlias(t *testing.T) {
	lib := contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/contract/v1",
		Domain:     "catalog",
		RPCs: map[string]*contract.RPCContract{
			"shop.catalog.v1.ProductService/GetProduct": {
				Aliases: map[string]*contract.AliasContract{
					"first":  {Fields: map[string]*contract.FieldContract{"id_product": {From: "shop.catalog.v1.ProductService/CreateProduct@first->product.id_product"}}},
					"second": {Fields: map[string]*contract.FieldContract{"id_product": {From: "shop.catalog.v1.ProductService/CreateProduct@second->product.id_product"}}},
				},
			},
		},
	}})
	got := contract.PrereqsFor(lib)("shop.catalog.v1.ProductService/GetProduct")
	scoped := map[string]string{}
	for _, p := range got {
		scoped[p.Node()] = p.For
	}
	if scoped["shop.catalog.v1.ProductService/CreateProduct@first"] != "first" ||
		scoped["shop.catalog.v1.ProductService/CreateProduct@second"] != "second" {
		t.Fatalf("each alias's edge must bind only a step of that alias, got %+v", got)
	}
}
