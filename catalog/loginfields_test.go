package catalog_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestLoginGuessNamesFieldsTheRequestActuallyHas(t *testing.T) {
	found := catalog.DetectLogins(catalogtest.New())
	if len(found) == 0 {
		t.Fatal("no login candidate in the fixture")
	}
	c := found[0]
	real := map[string]bool{}
	for _, f := range catalog.DescribeMessage(c.Method.Input()).Fields {
		real[f.Name] = true
	}
	for _, name := range []string{c.UserField, c.PasswordName} {
		if name == "" {
			continue
		}
		if !real[name] {
			t.Errorf("the guessed auth body names %q, which is not a field of %s. shrt writes this into "+
				"config.yaml, so the very first run dies with 'unknown field' — and the adopter has no "+
				"reason to suspect the guess rather than their own backend", name, c.Method.Input().FullName())
		}
	}
}
