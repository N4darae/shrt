package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestAReferenceMatchedOnlyByFoldingIsWarnedLikeAnExpectPath(t *testing.T) {
	for _, ref := range []string{"${create.ID}", "${create.Error.code}", "${steps.create.request.NAME}"} {
		c := &chain.Chain{Name: "folded-ref", Steps: []*chain.Step{
			{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget"},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
			{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": ref},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		}}
		if err := c.Normalize(); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, i := range chain.Lint(c, catalogtest.New()) {
			if i.Kind == chain.KindInexactPath && i.Step == "fetch" && i.Severity == chain.SeverityWarn && strings.Contains(i.Message, ref) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s resolves only by folding case or the JSON name, so lint warns inexact-path as for an expect path", ref)
		}
	}
}
