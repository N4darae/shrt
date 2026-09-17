package catalog_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestFieldAtWalksTheSchemaSkippingIndexes(t *testing.T) {
	cat := catalogtest.Batch()
	m, err := cat.Lookup("BatchService/Preview")
	if err != nil {
		t.Fatal(err)
	}
	out := catalog.DescribeMessage(m.Output()).Fields

	f, ok := catalog.FieldAt(out, chain.SplitPath("results.0.error.code"))
	if !ok || f == nil || f.Name != "code" {
		t.Fatalf("results.0.error.code should land on the code field, got %+v ok=%v", f, ok)
	}
	list, ok := catalog.FieldAt(out, chain.SplitPath("results"))
	if !ok || list == nil || !list.Repeated {
		t.Fatalf("results should be the repeated field itself, got %+v ok=%v", list, ok)
	}
	if _, ok := catalog.FieldAt(out, chain.SplitPath("results.error.no_such")); ok {
		t.Error("a missing leaf must not resolve")
	}
	if !catalog.HasPath(out, nil) {
		t.Error("the empty path names the message itself and is always present")
	}
	truncated := []*catalog.Field{{Name: "deep", Kind: "message", Truncated: true}}
	if f, ok := catalog.FieldAt(truncated, chain.SplitPath("deep.anything.at.all")); !ok || f == nil || f.Name != "deep" {
		t.Errorf("below a truncated field the walk cannot see, so it stops there and says yes: got %+v ok=%v", f, ok)
	}
}
