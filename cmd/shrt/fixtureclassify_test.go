package main

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAVarInsideOtherTextIsAFixtureNameOnlyWhenItIsolates(t *testing.T) {
	c := &chain.Chain{Name: "fx", Steps: []*chain.Step{
		{ID: "cp", Call: "CreateProduct", Body: map[string]any{"sku": "fx-${vars.tag}", "name": "F"}},
		{ID: "add", Call: "AddStock", Body: map[string]any{"id_product": "${cp.product.id_product}", "qty": "${vars.q}0"}},
		{ID: "get", Call: "GetCustomer", Body: map[string]any{"id_customer": "cus-${vars.n}"}},
		{ID: "order", Call: "CreateOrder", Body: map[string]any{"idempotency_key": "k1-${vars.tag}",
			"lines": []any{map[string]any{"note": "for ${vars.q}0"}}}},
	}}
	fixture := fixtureRequestPath(c)
	for _, tc := range []struct {
		step, path string
		want       bool
	}{
		{"cp", "sku", true},
		{"order", "idempotency_key", true},
		{"add", "qty", false},
		{"get", "id_customer", false},
		{"order", "lines.0.note", false},
	} {
		if got := fixture(tc.step, tc.path); got != tc.want {
			t.Errorf("%s %s: fixture=%v, want %v", tc.step, tc.path, got, tc.want)
		}
	}
}
