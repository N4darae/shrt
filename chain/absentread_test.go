package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func absentReadWarnings(t *testing.T, producer []chain.Expectation) []string {
	c := &chain.Chain{Name: "t", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", SkipAuth: true, Body: map[string]any{"name": "w"}, Expect: producer},
		{ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "${create.id}"},
			Expect: []chain.Expectation{{Path: "id", Equals: "${steps.create.response.id}"}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if strings.Contains(i.Message, "resolves to nothing") {
			out = append(out, i.Message)
		}
	}
	return out
}

func TestAReferenceToWhatItsStepExpectsRefusedOrAbsentIsWarned(t *testing.T) {
	refused := absentReadWarnings(t, []chain.Expectation{{Path: "transport.code", Equals: "permission_denied"}})
	if len(refused) != 2 || !strings.Contains(refused[0], "${create.id} reads what create expects to be refused") {
		t.Fatalf("a body and an expect reference into a step expected refused, want 2 warnings: %v", refused)
	}
	absent := absentReadWarnings(t, []chain.Expectation{{Path: "transport.code", Equals: "ok"}, {Path: "id", Exists: new(false)}})
	if len(absent) != 2 || !strings.Contains(absent[0], "expects absent (id exists: false)") {
		t.Fatalf("a reference to a field its step expects absent, want 2 warnings: %v", absent)
	}
	if got := absentReadWarnings(t, []chain.Expectation{{Path: "id", NotEmpty: true}}); len(got) != 0 {
		t.Fatalf("a step expected to answer the field is read safely: %v", got)
	}
	if got := absentReadWarnings(t, []chain.Expectation{{Path: "transport.code", Equals: "permission_denied"}, {Path: "id", Equals: "x"}}); len(got) != 0 {
		t.Fatalf("a refusal that still asserts the read field answers it: %v", got)
	}
}
