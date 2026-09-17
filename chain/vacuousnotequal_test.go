package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestNotEqualAgainstAnImpossibleValueDoesNotDeclareTheVerdict(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")

	refusals := []chain.ItemRefusal{{Path: "results.0.error.code", Code: "not_found"}}

	silencer := []chain.Expectation{
		{Path: "error.code", Equals: "OK"},
		{Path: "results.0.error.code", NotEqual: "ZZZ_THIS_VALUE_CAN_NEVER_OCCUR"},
	}
	if got := chain.UndeclaredRefusals(refusals, silencer); len(got) != 1 {
		t.Fatalf("not_equal against a value the verdict field can never hold passes whatever the backend "+
			"answered, so it declares nothing and must not suppress the refusal; got %v", got)
	}

	real := []chain.Expectation{{Path: "results.0.error.code", NotEqual: "OK"}}
	if got := chain.UndeclaredRefusals(refusals, real); len(got) != 0 {
		t.Fatalf("not_equal against the envelope OK value asserts that this line IS refused, which is a "+
			"real declaration and must still suppress the refusal; got %v", got)
	}
}

func TestVacuousNotEqualSparesDataPaths(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")

	cases := []struct {
		name string
		path string
		want any
		bad  bool
	}{
		{"empty string on an id discriminates", "id_deal", "", false},
		{"zero on a quantity discriminates", "rows.0.parked_out_receivable_qty", "0", false},
		{"a total discriminates", "pagination.total", "0", false},
		{"an impossible code on the item verdict does not", "results.3.error.code", "NEVER", true},
		{"the envelope OK value on the item verdict does", "results.3.error.code", "OK", false},
		{"an impossible code on the top-level envelope does not", "error.code", "NEVER", true},
	}
	for _, c := range cases {
		if got := chain.VacuousNotEqual(c.path, c.want); got != c.bad {
			t.Errorf("%s: VacuousNotEqual(%q, %v) = %v, want %v", c.name, c.path, c.want, got, c.bad)
		}
	}
}

func TestVacuousNotEqualResultCatchesAScalarComparedToAContainer(t *testing.T) {
	if !chain.VacuousNotEqualResult("assets", "NEVER_THIS", nil) {
		t.Error("a scalar compared against a field holding no scalar can never differ, so it discriminates nothing")
	}
	if !chain.VacuousNotEqualResult("assets", "NEVER_THIS", []any{}) {
		t.Error("a scalar compared against a list can never differ")
	}
	if chain.VacuousNotEqualResult("id_deal", "", "abc") {
		t.Error("a scalar compared against a scalar is a real assertion")
	}
}
