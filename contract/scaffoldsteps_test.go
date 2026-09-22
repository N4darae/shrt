package contract_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func wiredLibrary() *contract.Library {
	return contract.NewLibrary([]*contract.Overlay{{
		APIVersion: contract.OverlayAPIVersion,
		Domain:     "test",
		RPCs: map[string]*contract.RPCContract{
			"shrt.test.v1.ThingService/Create": {
				Summary:  "Creates a thing.",
				Status:   contract.StatusDraft,
				Required: []string{"name", "idempotency_key"},
				Fields: map[string]*contract.FieldContract{
					"idempotency_key": {Value: "${uuid}", CheckedBy: contract.CheckedByNone},
					"name":            {Value: "widget", CheckedBy: contract.CheckedByNone},
				},
			},
			"shrt.test.v1.ThingService/Fetch": {
				Summary:  "Reads a thing back.",
				Status:   contract.StatusDraft,
				Required: []string{"id"},
				Fields: map[string]*contract.FieldContract{
					"id": {From: "shrt.test.v1.ThingService/Create->id", CheckedBy: contract.CheckedByFK},
				},
			},
		},
	}})
}

func scaffold(t *testing.T, refs, ids []string) string {
	t.Helper()
	nodes, _, err := contract.ScaffoldSteps(refs, ids, wiredLibrary(), catalogtest.New())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := yaml.Marshal(&yaml.Node{Kind: yaml.SequenceNode, Content: nodes})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestScaffoldAppliesAValueTheContractAlreadyGives(t *testing.T) {
	out := scaffold(t, []string{"shrt.test.v1.ThingService/Create"}, []string{"create"})
	if strings.Contains(out, `idempotency_key: ""`) {
		t.Fatalf("the contract gives idempotency_key a value and the scaffold left it empty. A "+
			"scaffold taken from a repo that HAS a contract is then lint-red on the fields the "+
			"contract already answered, and only 'contract plan' used the wiring:\n%s", out)
	}
	if !strings.Contains(out, "${uuid}") || !strings.Contains(out, "widget") {
		t.Errorf("both contract values should reach the body:\n%s", out)
	}
	if !strings.Contains(out, "Creates a thing.") {
		t.Errorf("the summary should become the step description:\n%s", out)
	}
}

func TestScaffoldWiresFromBetweenTheStepsItWasGiven(t *testing.T) {
	out := scaffold(t,
		[]string{"shrt.test.v1.ThingService/Create", "shrt.test.v1.ThingService/Fetch"},
		[]string{"create", "fetch"})
	if !strings.Contains(out, "${create.id}") {
		t.Fatalf("Fetch's id comes from Create, and Create is one of the steps being scaffolded, so "+
			"the reference is resolvable without a plan:\n%s", out)
	}
}

func TestScaffoldSaysWhichProducerIsMissing(t *testing.T) {
	_, notes, err := contract.ScaffoldSteps(
		[]string{"shrt.test.v1.ThingService/Fetch"}, []string{"fetch"}, wiredLibrary(), catalogtest.New())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "ThingService/Create") {
		t.Fatalf("scaffolding Fetch alone cannot resolve id, and the note has to name the rpc that "+
			"produces it — otherwise the empty field reads as the contract having nothing to say: %q", joined)
	}
}

func TestScaffoldWithoutALibraryStillProducesAStep(t *testing.T) {
	nodes, _, err := contract.ScaffoldSteps(
		[]string{"shrt.test.v1.ThingService/Create"}, []string{"create"}, nil, catalogtest.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("a repo with no contract library must still scaffold: got %d nodes", len(nodes))
	}
}
