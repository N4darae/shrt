package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func declaring(vars map[string]any) *chain.Chain {
	return &chain.Chain{Name: "c", Vars: vars}
}

func TestANumericOverrideStaysAStringWhenTheChainDeclaredOne(t *testing.T) {
	c := declaring(map[string]any{"observed_at": "1788868800"})

	got := c.CoerceVars(map[string]any{"observed_at": int64(1790062486)})

	if got["observed_at"] != "1790062486" {
		t.Fatalf("the proto field is a string, and -var inferred an int64 from a run line the chain's "+
			"own description tells you to use: got %#v", got["observed_at"])
	}
}

func TestAStringOverrideBecomesANumberWhenTheChainDeclaredOne(t *testing.T) {
	c := declaring(map[string]any{"page_size": 10})

	got := c.CoerceVars(map[string]any{"page_size": "25"})

	if got["page_size"] != int64(25) {
		t.Fatalf("a chain that declares a number wants a number: got %#v", got["page_size"])
	}
}

func TestABooleanDeclarationTakesATextOverride(t *testing.T) {
	c := declaring(map[string]any{"dry": false})

	got := c.CoerceVars(map[string]any{"dry": "true"})

	if got["dry"] != true {
		t.Fatalf("got %#v", got["dry"])
	}
}

func TestAnUndeclaredVarIsLeftExactlyAsSupplied(t *testing.T) {
	c := declaring(map[string]any{"tag": "SPOT"})

	got := c.CoerceVars(map[string]any{"tag": "X", "not_declared": int64(7)})

	if got["not_declared"] != int64(7) {
		t.Fatalf("with nothing declaring a type there is nothing to coerce to, and guessing would be "+
			"the very thing this fixes: got %#v", got["not_declared"])
	}
}

func TestAValueThatCannotBecomeTheDeclaredTypeIsLeftAlone(t *testing.T) {
	c := declaring(map[string]any{"page_size": 10})

	got := c.CoerceVars(map[string]any{"page_size": "not a number"})

	if got["page_size"] != "not a number" {
		t.Fatalf("silently substituting a zero would turn a typo into a different query: got %#v",
			got["page_size"])
	}
}

func TestCoercionDoesNotInventKeysOrDropThem(t *testing.T) {
	c := declaring(map[string]any{"a": "1", "b": "2"})

	got := c.CoerceVars(map[string]any{"a": int64(9)})

	if len(got) != 1 {
		t.Fatalf("only what was supplied is returned; a declared-but-unsupplied var keeps its own "+
			"default in the chain: %#v", got)
	}
}

func TestAChainThatDeclaresNoVarsPassesEverythingThrough(t *testing.T) {
	c := declaring(nil)

	got := c.CoerceVars(map[string]any{"x": int64(1)})

	if got["x"] != int64(1) {
		t.Fatalf("got %#v", got["x"])
	}
}
