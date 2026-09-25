package catalog_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestLookupOfAMisspelledRpcSuggestsTheCloseOne(t *testing.T) {
	cat := catalogtest.New()
	for _, ref := range []string{"ThingServce/Create", "shrt.test.v1.ThingService/Creat", "Creat"} {
		_, err := cat.Lookup(ref)
		if err == nil || !errors.Is(err, catalog.ErrNotFound) {
			t.Fatalf("lookup %q: want not found, got %v", ref, err)
		}
		if !strings.Contains(err.Error(), "did you mean") || !strings.Contains(err.Error(), "shrt.test.v1.ThingService/Create") {
			t.Fatalf("lookup %q: a near miss must name the rpc it is close to, got %v", ref, err)
		}
	}
	_, err := cat.Lookup("NoSuchRpcAtAll")
	if err == nil || strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("a name close to nothing gets no suggestion, got %v", err)
	}
}

func TestLookupDoesNotSuggestAnRpcWhoseOwnNameIsNotClose(t *testing.T) {
	cat := catalogtest.New()
	_, err := cat.Lookup("shrt.test.v1.ThingService/Delete")
	if err == nil || strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("a long shared prefix does not make Delete close to Create, got %v", err)
	}
}
