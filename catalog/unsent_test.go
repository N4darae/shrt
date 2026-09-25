package catalog_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestUnsentDefaultOnlyForProto3ZeroValues(t *testing.T) {
	cat := catalogtest.New()
	m, err := cat.Lookup("ThingService/Fetch")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		v    any
		want bool
	}{
		{"total", float64(0), true},
		{"total", float64(3), false},
		{"name", "", true},
		{"name", "x", false},
		{"error", nil, true},
		{"error", map[string]any{"code": ""}, false},
		{"error.code", "", true},
		{"nope", "", false},
	}
	for _, c := range cases {
		if got := catalog.UnsentDefault(m.Output(), c.path, c.v); got != c.want {
			t.Errorf("UnsentDefault(%s, %v) = %v, want %v", c.path, c.v, got, c.want)
		}
	}
}
