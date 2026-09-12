package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func partialLibrary() *contract.Library {
	return contract.NewLibrary([]*contract.Overlay{{
		Domain: "test",
		RPCs: map[string]*contract.RPCContract{
			"shrt.test.v1.ThingService/Fetch": {
				Summary: "read one thing",
				Status:  contract.StatusDraft,
				Fields: map[string]*contract.FieldContract{
					"id": {From: "shrt.test.v1.ThingService/Create@base->id"},
				},
				Needs: []string{"shrt.test.v1.PartnerService/FetchMine"},
			},
		},
	}})
}

func TestLintNamesTheRPCsItCouldNotReach(t *testing.T) {
	cat := catalogtest.New()
	reach := contract.ReferencedOutsideLibrary(partialLibrary(), cat, "")
	if reach.Empty() {
		t.Fatal("Create and FetchMine are referenced and absent from the library; lint must say so")
	}
	want := []string{
		"shrt.test.v1.ThingService/Create",
		"shrt.test.v1.PartnerService/FetchMine",
	}
	for _, rpc := range want {
		if !contains(reach.RPCs, rpc) {
			t.Errorf("out-of-reach list omits %s: %v", rpc, reach.RPCs)
		}
	}
	if reach.Sites != 2 {
		t.Errorf("want 2 reference sites, got %d", reach.Sites)
	}
	if len(reach.Domains) != 1 || reach.Domains[0] != "test" {
		t.Errorf("the domain list is what tells a new repo which overlay to author next, got %v", reach.Domains)
	}
}

func TestAnRPCPresentInTheLibraryIsNotOutOfReach(t *testing.T) {
	cat := catalogtest.New()
	lib := contract.NewLibrary([]*contract.Overlay{{
		Domain: "test",
		RPCs: map[string]*contract.RPCContract{
			"shrt.test.v1.ThingService/Create": {Summary: "write", Status: contract.StatusDraft},
			"shrt.test.v1.ThingService/Fetch": {
				Summary: "read", Status: contract.StatusDraft,
				Fields: map[string]*contract.FieldContract{
					"id": {From: "shrt.test.v1.ThingService/Create->id"},
				},
			},
		},
	}})
	if reach := contract.ReferencedOutsideLibrary(lib, cat, ""); !reach.Empty() {
		t.Fatalf("nothing is out of reach here, got %v", reach.RPCs)
	}
}

func TestOutOfReachIsSilentAboutAnRPCTheDescriptorDoesNotKnow(t *testing.T) {
	cat := catalogtest.New()
	lib := contract.NewLibrary([]*contract.Overlay{{
		Domain: "test",
		RPCs: map[string]*contract.RPCContract{
			"shrt.test.v1.ThingService/Fetch": {
				Summary: "read", Status: contract.StatusDraft,
				Needs: []string{"shrt.test.v1.GhostService/Vanished"},
			},
		},
	}})
	if reach := contract.ReferencedOutsideLibrary(lib, cat, ""); !reach.Empty() {
		t.Fatalf("an unknown rpc is already a lint ERROR; the note must not double-report it as merely unreachable: %v", reach.RPCs)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle || strings.HasSuffix(s, needle) {
			return true
		}
	}
	return false
}
