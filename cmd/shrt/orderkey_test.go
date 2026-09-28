package main

import (
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestOrderKeyNamesTheKeyTheNewOrderFollows(t *testing.T) {
	st := &runner.StepRecord{ID: "list", Response: []byte(`{"products":[{"id_product":"p2","sku":"B","name":"a"},{"id_product":"p1","sku":"A","name":"b"}]}`)}
	was := map[string]any{"products.0.sku": "A", "products.0.name": "b", "products.1.sku": "B", "products.1.name": "a"}
	for _, tc := range []struct {
		name string
		a    attribution
		want string
	}{
		{"was by sku, now by name", attribution{was: func(step, path string) (any, bool) { v, ok := was[path]; return v, ok }}, "now by name, was by sku"},
		{"no earlier values", attribution{was: func(string, string) (any, bool) { return nil, false }}, "now by name"},
		{"no was func", attribution{}, ""},
	} {
		if got := tc.a.orderKey(st, "products.1.name"); got != tc.want {
			t.Errorf("%s: orderKey = %q, want %q", tc.name, got, tc.want)
		}
	}
}
