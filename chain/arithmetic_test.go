package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func stockCheck(want any) *chain.Chain {
	ok := chain.Expectation{Path: "status.code", Equals: "SUCCESS"}
	c := &chain.Chain{Name: "arith", Steps: []*chain.Step{
		{ID: "a", Call: "shop.catalog.v1.StockService/AddStock",
			Body: map[string]any{"id_product": "p", "qty": "2"}, Expect: []chain.Expectation{ok}},
		{ID: "b", Call: "shop.catalog.v1.StockService/AddStock",
			Body:   map[string]any{"id_product": "p", "qty": "3"},
			Expect: []chain.Expectation{ok, {Path: "qty_on_hand", Equals: want}}},
	}}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func arithmeticIssues(c *chain.Chain) []chain.Issue {
	out := []chain.Issue{}
	for _, i := range chain.Lint(c, catalogtest.Shop()) {
		if i.Kind == chain.KindArithmetic {
			out = append(out, i)
		}
	}
	return out
}

func TestInterpolatedArithmeticAgainstANumericFieldIsFlagged(t *testing.T) {
	for _, want := range []string{
		"${a.qty_on_hand}+${steps.b.request.qty}",
		"${a.qty_on_hand} + 3",
		"${a.qty_on_hand}*2",
		"10-${a.qty_on_hand}",
	} {
		got := arithmeticIssues(stockCheck(want))
		if len(got) != 1 || got[0].IsError() || !strings.Contains(got[0].Message, "qty_on_hand") {
			t.Fatalf("equals: %q on an int64 compares text that no number equals; want one warning, got %+v", want, got)
		}
		strict := chain.Promote(got, chain.IsAssertionQualityIssue)
		if !strict[0].IsError() {
			t.Fatalf("-strict must fail interpolated arithmetic, got %+v", strict)
		}
	}
}

func TestAPlainReferenceOrNumberIsNotArithmetic(t *testing.T) {
	for _, want := range []any{"${a.qty_on_hand}", "5", 5, "-5", "${vars.n}"} {
		c := stockCheck(want)
		c.Vars = map[string]any{"n": "5"}
		if got := arithmeticIssues(c); len(got) != 0 {
			t.Fatalf("equals: %v is not arithmetic, got %+v", want, got)
		}
	}
}
