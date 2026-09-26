package catalog_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestARequestWithAnUnknownFieldOrEnumValueNamesTheValidOnes(t *testing.T) {
	cat := catalogtest.New()
	m, err := cat.Lookup("ThingService/Create")
	if err != nil {
		t.Fatal(err)
	}
	err = cat.ValidateInput(m, []byte(`{"nme":"w","kind":"KIND_A"}`))
	if err == nil || !strings.Contains(err.Error(), `"nme" is not a field of`) || !strings.Contains(err.Error(), "valid fields: ") {
		t.Fatalf("an unknown field names the closest real one, got %v", err)
	}
	err = cat.ValidateInput(m, []byte(`{"name":"w","kind":"KIND_Z"}`))
	if err == nil || !strings.Contains(err.Error(), `"KIND_Z" is not a value of`) || !strings.Contains(err.Error(), "KIND_A") {
		t.Fatalf("an invalid enum value lists the values, got %v", err)
	}
}
