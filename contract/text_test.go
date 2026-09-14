package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func textLibrary() (*contract.Library, string) {
	rpc := "shrt.test.v1.ThingService/Create"
	return contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/contract/v1",
		Domain:     "test",
		Failures:   []contract.Failure{{Code: 1001, Reason: "Unauthenticated", When: "no token"}},
		RPCs: map[string]*contract.RPCContract{
			rpc: {
				Summary:      "Create a thing.",
				Status:       contract.StatusDraft,
				RequiresRole: []string{"ADMIN"},
				Required:     []string{"name"},
				Fields: map[string]*contract.FieldContract{
					"name":            {Note: "free text"},
					"idempotency_key": {Value: "${uuid}"},
				},
				Exports:  map[string]string{"id": "the new thing"},
				Failures: []contract.Failure{{Code: 1200, Reason: "NameTaken", When: "the name already exists"}},
			},
		},
	}}), rpc
}

func TestRPCContractTextRendersEveryCuratedKeyAnAuthorWrote(t *testing.T) {
	lib, rpc := textLibrary()
	c, ok := lib.Get(rpc)
	if !ok {
		t.Fatal("fixture contract not found")
	}
	got := c.Text(lib, rpc)

	for _, want := range []string{
		"CURATED CONTRACT",
		"Create a thing.",
		"ADMIN",
		"name",
		"${uuid}",
		"1200",
		"NameTaken",
		"the name already exists",
		"1001",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered contract omits %q — a reader sees only what this prints, so an omitted "+
				"key reads as a key nobody wrote:\n%s", want, got)
		}
	}
}

func TestRPCContractTextIsReachableFromTheLibraryNotOnlyTheCLI(t *testing.T) {
	lib, rpc := textLibrary()
	c, _ := lib.Get(rpc)
	if c.Text(lib, rpc) == "" {
		t.Fatal("Text returned nothing: it lived in package main until now, so a library consumer " +
			"could print the generated contract and not the curated one")
	}
}
