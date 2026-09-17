package catalog_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestDetectEnvelopeFindsTheDefaultShapeInTheFixture(t *testing.T) {
	found := catalog.DetectEnvelope(catalogtest.New())
	if len(found) == 0 {
		t.Fatal("no envelope detected in a fixture whose responses all carry error.code — an adopter " +
			"whose backend differs is told nothing, and every scaffolded assertion silently points at a " +
			"path their responses do not have")
	}
	if found[0].Path != "error.code" {
		t.Errorf("best candidate = %q, want error.code", found[0].Path)
	}
	if found[0].Field != "error" {
		t.Errorf("field = %q, want error — it is the first segment of the path", found[0].Field)
	}
}

func TestDetectEnvelopeIgnoresAShapeMostResponsesDoNotCarry(t *testing.T) {
	found := catalog.DetectEnvelope(catalogtest.Minority())
	offered := map[string]int{}
	for _, c := range found {
		offered[c.Path] = c.Count
	}
	if offered["error.code"] != 3 {
		t.Errorf("error.code is carried by 3 of the 4 responses and must be offered with that count, got %v", offered)
	}
	if n, ok := offered["status.code"]; ok {
		t.Errorf("status.code is carried by %d of 4 responses — a field a minority carry is not the "+
			"envelope, and offering it points every scaffolded assertion at a path most responses lack", n)
	}
	if len(found) != 1 {
		t.Errorf("want exactly the one majority candidate, got %v", offered)
	}
}

func TestDetectItemEnvelopeRepeatsTheConfiguredEnvelopeOnly(t *testing.T) {
	found := catalog.DetectItemEnvelope(catalogtest.Listing(), "result.code")
	if len(found) == 0 {
		t.Fatal("entries[].result.code repeats the envelope per item and was not found")
	}
	if found[0].Path != "entries[].result.code" {
		t.Errorf("the element whose result is the SAME message as the top-level envelope ranks first, got %+v", found)
	}
	for _, c := range found {
		if strings.HasSuffix(c.Path, "].status") {
			t.Errorf("a scalar named status is data, not a verdict: %+v", found)
		}
	}
}
