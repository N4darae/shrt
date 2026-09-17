package hollow_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestNotEqualAgainstAValueTheFieldCanHoldStillRescuesTheStep(t *testing.T) {
	rep := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "pagination.total", Rule: "not_equal", Want: "0", Got: "7", Passed: true},
	})
	if rep.Unallowed != 0 {
		t.Fatalf("want 0 hollow steps, got %d — GRAMMAR.md lists not_equal on a scalar the field really "+
			"holds as a rule that fires, and the corpus uses it on counts and money; calling every "+
			"not_equal vacuous reports those steps hollow and pushes authors back toward filler",
			rep.Unallowed)
	}
}

func TestNotEqualIsJudgedByItsValueNotItsName(t *testing.T) {
	vacuous := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "assets", Rule: "not_equal", Want: "NEVER_THIS", Got: nil, Passed: true},
	})
	real := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "assets.0.id_asset", Rule: "not_equal", Want: "", Got: "a-1", Passed: true},
	})
	if vacuous.Unallowed != 1 || real.Unallowed != 0 {
		t.Fatalf("the same rule name must split on the compared value: impossible value hollow=%d (want 1), "+
			"real value hollow=%d (want 0)", vacuous.Unallowed, real.Unallowed)
	}
}
