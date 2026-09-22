package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func TestExportablePathsIndexRepeatedFields(t *testing.T) {
	m, err := catalogtest.Batch().Lookup("BatchService/Preview")
	if err != nil {
		t.Fatal(err)
	}
	text := contract.For(m).Text()
	if !strings.Contains(text, "results.0.amount") {
		t.Fatalf("EXPORTABLE PATHS must print a path a chain can actually use. 'results' is a list, "+
			"so every path under it needs an element, and the un-indexed form lints clean and then "+
			"fails at run time with 'path not present in response':\n%s", text)
	}
	if strings.Contains(text, "  results.amount ") {
		t.Error("the un-indexed form must not be printed at all")
	}
}
