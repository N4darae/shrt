package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func verdictIssues(e chain.Expectation) []chain.Issue {
	c := &chain.Chain{Name: "t", Steps: []*chain.Step{{
		ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true,
		Expect: []chain.Expectation{e, {Path: "id_deal", NotEmpty: true}},
	}}}
	_ = c.Normalize()
	out := []chain.Issue{}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if i.Kind == chain.KindUnfailable && strings.Contains(i.Message, "not_equal") {
			out = append(out, i)
		}
	}
	return out
}

func TestNotEqualEmptyOnTheVerdictIsReported(t *testing.T) {
	got := verdictIssues(chain.Expectation{Path: "error.code", NotEqual: ""})
	if len(got) != 1 {
		t.Fatalf("not_equal \"\" on the verdict holds on the ok value and on every refusal, so it declares "+
			"nothing and -strict must fail it; got %v", got)
	}
	if got[0].IsError() {
		t.Fatalf("an assertion-quality finding is a warning -strict promotes, got %v", got[0])
	}
}

func TestNotEqualOnTheVerdictAgainstARealValueIsLeftAlone(t *testing.T) {
	for _, v := range []any{"OK", "internal"} {
		if got := verdictIssues(chain.Expectation{Path: "error.code", NotEqual: v}); len(got) != 0 {
			t.Errorf("not_equal %v names a value the verdict can hold, got %v", v, got)
		}
	}
	if got := verdictIssues(chain.Expectation{Path: "id_deal", NotEqual: ""}); len(got) != 0 {
		t.Errorf("not_equal \"\" on a data field discriminates, got %v", got)
	}
}
