package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestSyntheticLeniencyDoesNotLeakIntoARealRun(t *testing.T) {
	real := map[string]any{"results": []any{map[string]any{"id": "a"}}}

	s := chain.NewScope(nil)
	s.Record("mk", nil, real)
	if _, err := s.ResolveValue("${mk.results.1.id}"); err == nil {
		t.Fatal("a REAL response resolved an out-of-range index — dry-run leniency leaked into a real run, " +
			"so a chain would silently reuse row 0 where it meant row 1")
	}

	d := chain.NewScope(nil)
	d.RecordSynthetic("mk", nil, real)
	v, err := d.ResolveValue("${mk.results.1.id}")
	if err != nil {
		t.Fatalf("a synthesized response refused an index it has no real length for: %v", err)
	}
	if v != "a" {
		t.Fatalf("got %v, want the synthesized element", v)
	}
}

func TestSyntheticLeniencyStillRefusesAFieldThatDoesNotExist(t *testing.T) {
	d := chain.NewScope(nil)
	d.RecordSynthetic("mk", nil, map[string]any{"results": []any{map[string]any{"id": "a"}}})
	if _, err := d.ResolveValue("${mk.results.0.no_such_field}"); err == nil {
		t.Fatal("synthetic leniency excused a missing FIELD — it must only excuse a list index, " +
			"or dry-run stops catching typos, which is most of what it is for")
	}
}
