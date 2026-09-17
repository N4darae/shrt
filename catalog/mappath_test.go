package catalog_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog"
)

func TestHasPathReachesIntoAMapValue(t *testing.T) {
	fields := []*catalog.Field{
		{Name: "error", Kind: "message", Fields: []*catalog.Field{
			{Name: "details", Kind: "message", Repeated: true, Fields: []*catalog.Field{
				{Name: "meta", Kind: "map<string, string>", MapKey: "string"},
			}},
		}},
	}
	if !catalog.HasPath(fields, []string{"error", "details", "0", "meta", "constraint"}) {
		t.Error("a map key is not declared in the descriptor, so any key under a map field is a field that " +
			"exists; reporting it absent makes lint reject a path the backend really carries")
	}
	if catalog.HasPath(fields, []string{"error", "details", "0", "absent", "x"}) {
		t.Error("a path that is not a field must still be reported absent")
	}
}
