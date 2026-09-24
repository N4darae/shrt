package main

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAFixtureOnlyVarIsOneReadOnlyInsideAFixtureName(t *testing.T) {
	c := &chain.Chain{Name: "c", Vars: map[string]any{"tag": "t", "qty": 1, "both": "b"}, Steps: []*chain.Step{
		{ID: "create", Call: "S/Create", Body: map[string]any{"sku": "sku-${vars.tag}", "name": "Widget ${vars.both}", "qty": "${vars.qty}"}},
		{ID: "find", Call: "S/Find", Body: map[string]any{"sku": "${vars.both}"}},
	}}
	fixture := fixtureOnlyVar(c)
	for name, want := range map[string]bool{"tag": true, "qty": false, "both": false, "missing": false} {
		if got := fixture(name); got != want {
			t.Errorf("fixtureOnlyVar(%q) = %v, want %v", name, got, want)
		}
	}
}
