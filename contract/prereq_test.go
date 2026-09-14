package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/contract"
)

func prereqLibrary() *contract.Library {
	return contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/contract/v1",
		Domain:     "test",
		RPCs: map[string]*contract.RPCContract{
			"shrt.test.v1.ThingService/Create": {
				Needs: []string{"shrt.test.v1.AuthService/Login"},
				Fields: map[string]*contract.FieldContract{
					"id_owner": {From: "shrt.test.v1.PartnerService/CreatePartner->id"},
					"ref":      {SameAs: "shrt.test.v1.PartnerService/CreatePartner->name"},
				},
			},
		},
	}})
}

func TestPrereqsForIsReachableFromTheLibraryNotOnlyTheCLI(t *testing.T) {
	got := contract.PrereqsFor(prereqLibrary())("shrt.test.v1.ThingService/Create")
	if len(got) == 0 {
		t.Fatal("no prerequisites resolved: chain.Slice takes this function, so a library consumer who " +
			"cannot build it gets zero unmet prerequisites reported and a slice that looks self-contained " +
			"when it is not")
	}
	edges := map[string]string{}
	for _, p := range got {
		edges[p.RPC] = p.Edge
	}
	if edges["shrt.test.v1.AuthService/Login"] != "needs" {
		t.Errorf("a needs: entry did not become a prerequisite, got %+v", got)
	}
	if _, ok := edges["shrt.test.v1.PartnerService/CreatePartner"]; !ok {
		t.Errorf("neither the from: nor the same_as: edge became a prerequisite, got %+v", got)
	}
}

func TestPrereqsForDoesNotMakeAnRPCItsOwnPrerequisite(t *testing.T) {
	lib := contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/contract/v1",
		Domain:     "test",
		RPCs: map[string]*contract.RPCContract{
			"shrt.test.v1.ThingService/Create": {
				Fields: map[string]*contract.FieldContract{
					"parent": {From: "shrt.test.v1.ThingService/Create->id"},
				},
			},
		},
	}})
	if got := contract.PrereqsFor(lib)("shrt.test.v1.ThingService/Create"); len(got) != 0 {
		t.Errorf("a self-referencing from: became a prerequisite on itself, which no slice can ever "+
			"satisfy: %+v", got)
	}
}
