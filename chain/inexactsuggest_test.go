package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestAnInexactReferenceSuggestsADocumentedForm(t *testing.T) {
	cases := map[string]string{
		"${steps.create.response.ID}":    "write ${steps.create.response.id}",
		"${steps.create.ERROR.code}":     "write ${steps.create.response.error.code}",
		"${steps.create.request.NAME}":   "write ${steps.create.request.name}",
		"${create.Error.code}":           "write ${create.error.code}",
		"${create.response.Error.code}":  "write ${create.error.code}",
		"${steps.create.response.Error}": "write ${steps.create.response.error}",
	}
	for ref, want := range cases {
		c := &chain.Chain{Name: "folded-ref", Steps: []*chain.Step{
			{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget"},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
			{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": ref},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		}}
		if err := c.Normalize(); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, i := range chain.Lint(c, catalogtest.New()) {
			if i.Kind == chain.KindInexactPath && i.Step == "fetch" {
				got = append(got, i.Message)
			}
		}
		ok := false
		for _, m := range got {
			ok = ok || strings.HasSuffix(m, want)
		}
		if !ok {
			t.Errorf("%s: want a suggestion ending %q (a form GRAMMAR section 2 documents), got %q", ref, want, got)
		}
	}
}
