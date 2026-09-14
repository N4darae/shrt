package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func lintStep(s *chain.Step) []chain.Issue {
	c := &chain.Chain{Name: "t", Steps: []*chain.Step{s}}
	_ = c.Normalize()
	out := []chain.Issue{}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if strings.Contains(i.Message, "cannot fail") {
			out = append(out, i)
		}
	}
	return out
}

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

func TestAnAssertionThatCannotFailIsReported(t *testing.T) {
	got := lintStep(&chain.Step{
		ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true,
		Expect: []chain.Expectation{{Path: "error.code", Exists: yes()}},
	})
	if len(got) != 1 {
		t.Fatalf("want 1 issue, got %d: exists:true on the envelope holds for every well-formed "+
			"response, so the step is green whatever the server did — and no gate downstream can see it", len(got))
	}
}

func TestAssertionsThatCanFailAreLeftAlone(t *testing.T) {
	cases := []chain.Expectation{
		{Path: "error.code", Equals: "OK"},
		{Path: "error.code", NotEqual: "internal"},
		{Path: "error.code", Exists: no()},
		{Path: "id_deal", Exists: yes()},
		{Path: "id_deal", NotEmpty: true},
	}
	for _, e := range cases {
		if got := lintStep(&chain.Step{
			ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true,
			Expect: []chain.Expectation{e},
		}); len(got) != 0 {
			t.Errorf("%+v reported as unfailable, but it can fail — an unhandled panic really does "+
				"return internal, and a missing id really does fail exists/not_empty: %v", e, got)
		}
	}
}
