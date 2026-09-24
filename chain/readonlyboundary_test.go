package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAReadPrefixCountsOnlyAtAWordBoundary(t *testing.T) {
	for call, want := range map[string]bool{
		"ShowcaseProduct":                  false,
		"Getaway":                          false,
		"Listen":                           false,
		"Counterfeit":                      false,
		"shop.v1.PromoService/ShowcaseFoo": false,
		"GetProduct":                       true,
		"Get":                              true,
		"Get2Product":                      true,
		"ListProducts":                     true,
		"shop.v1.CatalogService/ShowItem":  true,
		"Get_product":                      true,
	} {
		if got := chain.IsReadOnlyCall(call); got != want {
			t.Errorf("IsReadOnlyCall(%q) = %v, want %v", call, got, want)
		}
	}
}
