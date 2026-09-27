package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestAVolatilePatternOnIdShapedPathsIsWarnedOnce(t *testing.T) {
	c := &chain.Chain{Name: "t", Volatile: []string{"**.id_product", "**.created_at", "**.*_id"}, Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", SkipAuth: true, Body: map[string]any{"name": "w"},
			Volatile: []string{"**.id"}, Expect: []chain.Expectation{{Path: "id", NotEmpty: true}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	got := []chain.Issue{}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if strings.Contains(i.Message, "masks id-shaped paths") {
			got = append(got, i)
		}
	}
	if len(got) != 2 || got[0].Step != "" || got[1].Step != "create" {
		t.Fatalf("one warning for the chain's id patterns and one for the step's, got %v", got)
	}
	if !strings.Contains(got[0].Message, `"**.id_product", "**.*_id" masks`) || strings.Contains(got[0].Message, "created_at") ||
		!strings.Contains(got[0].Message, "verify already pairs ids across runs") {
		t.Fatalf("the warning names the id patterns only and why they hurt: %s", got[0].Message)
	}
}
