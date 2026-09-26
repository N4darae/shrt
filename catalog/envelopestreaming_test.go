package catalog_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestDetectEnvelopeCountsUnaryRPCsOnly(t *testing.T) {
	found := catalog.DetectEnvelope(catalogtest.Rich())
	if len(found) == 0 || found[0].Path != "error.code" {
		t.Fatalf("PlaceOrder answers with error.code, want it offered: %+v", found)
	}
	if found[0].Count != 1 || found[0].Of != 1 {
		t.Fatalf("shrt never calls a streaming rpc, so only the one unary rpc counts: %d of %d", found[0].Count, found[0].Of)
	}
}
