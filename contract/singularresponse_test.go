package contract_test

import (
	"slices"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func TestASingularMessageResponseFieldIsChargedLikeARepeatedOne(t *testing.T) {
	path, ok := chain.EnvelopePath(), chain.EnvelopeOK()
	contract.ApplyConventions(nil, "status.code", "SUCCESS")
	t.Cleanup(func() { contract.ApplyConventions(nil, path, ok) })
	shape := contract.MethodShapes(catalogtest.Shop())["shop.catalog.v1.ProductService/GetProduct"]
	if !slices.Contains(shape.ResponseFields, "product") {
		t.Fatalf("product is a response field a contract can declare, got %v", shape.ResponseFields)
	}
	if slices.Contains(shape.ResponseFields, "status") {
		t.Fatalf("the envelope is not charged, got %v", shape.ResponseFields)
	}
}
