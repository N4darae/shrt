package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func planWith(t *testing.T, producer, consumer *contract.FieldContract) string {
	t.Helper()
	lib := contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/contract/v1", Domain: "test",
		RPCs: map[string]*contract.RPCContract{
			"shrt.test.v1.ThingService/Create": {
				Fields: map[string]*contract.FieldContract{"name": producer},
			},
			"shrt.test.v1.ThingService/Fetch": {
				Fields: map[string]*contract.FieldContract{"id": consumer},
			},
		},
	}})
	p, err := contract.BuildPlan("shrt.test.v1.ThingService/Fetch", lib, catalogtest.New(), "t")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.YAML()
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestSameAsReusesTheProducersOwnReferenceInsteadOfBlankingIt(t *testing.T) {
	out := planWith(t,
		&contract.FieldContract{Value: "${steps.earlier.response.id}"},
		&contract.FieldContract{SameAs: "shrt.test.v1.ThingService/Create->name"})

	if strings.Contains(out, `${vars.create_name}`) {
		t.Fatalf("same_as overwrote a working reference with an empty var. Both sites end up reading a "+
			"var nobody can fill, because the value is minted by the run:\n%s", out)
	}
	if strings.Count(out, "${steps.earlier.response.id}") != 2 {
		t.Errorf("want both sites reading the producer's own reference — that keeps them equal AND wired:\n%s", out)
	}
}

func TestSameAsStillSharesAVarWhenTheProducerValueIsGenerated(t *testing.T) {
	out := planWith(t,
		&contract.FieldContract{Value: "${uuid}"},
		&contract.FieldContract{SameAs: "shrt.test.v1.ThingService/Create->name"})

	if strings.Count(out, "${uuid}") > 1 {
		t.Errorf("${uuid} mints a fresh value at every resolution, so sharing the expression verbatim "+
			"makes the two sites DIFFER — the one thing same_as exists to prevent:\n%s", out)
	}
	if !strings.Contains(out, "${vars.") {
		t.Errorf("a generated producer value must still bind through one shared var:\n%s", out)
	}
}

func TestSameAsStillSharesAVarWhenTheProducerValueIsAuthorSupplied(t *testing.T) {
	out := planWith(t, &contract.FieldContract{},
		&contract.FieldContract{SameAs: "shrt.test.v1.ThingService/Create->name"})

	if !strings.Contains(out, "${vars.") {
		t.Errorf("with no producer edge the value is the author's to supply, and one shared var is how "+
			"both sites are held equal:\n%s", out)
	}
}
