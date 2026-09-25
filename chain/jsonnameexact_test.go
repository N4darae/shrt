package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestAFieldSpelledAsItsJSONNameIsExact(t *testing.T) {
	c := &chain.Chain{Name: "json-names", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "idempotency_key": "k-1"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${steps.create.request.idempotencyKey}"},
			Export: map[string]string{"when": "createdAt"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}, {Path: "createdAt", NotEmpty: true}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if i.Kind == chain.KindInexactPath {
			t.Errorf("idempotencyKey and createdAt are the protojson JSON names of their fields, the same field (GRAMMAR section 7): %+v", i)
		}
	}

	c.Steps[1].Body["id"] = "${steps.create.request.IdempotencyKey}"
	found := false
	for _, i := range chain.Lint(c, catalogtest.New()) {
		found = found || i.Kind == chain.KindInexactPath && i.Step == "fetch"
	}
	if !found {
		t.Fatal("IdempotencyKey is neither the proto name nor the JSON name, so it still matches only by folding case")
	}
}
