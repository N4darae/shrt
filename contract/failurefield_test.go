package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func lintFailures(t *testing.T, failures []Failure) []contract.Issue {
	t.Helper()
	lib := &contract.Library{Overlays: []*contract.Overlay{{
		APIVersion: contract.OverlayAPIVersion,
		Domain:     "test",
		RPCs: map[string]*contract.RPCContract{
			"shrt.test.v1.ThingService/Create": {
				Summary:  "creates a thing",
				Status:   contract.StatusDraft,
				Required: []string{"name"},
				Failures: failures,
			},
		},
	}}}
	return contract.LintLibrary(lib, catalogtest.New())
}

type Failure = contract.Failure

func TestTwoFailuresDifferingOnlyInFieldAreNotADuplicate(t *testing.T) {
	issues := lintFailures(t, []Failure{
		{Code: 1105, Reason: "MissingField", Field: "name", When: "name is empty"},
		{Code: 1105, Reason: "MissingField", Field: "kind", When: "kind is unspecified"},
	})
	for _, i := range issues {
		if strings.Contains(i.Message, "repeats") {
			t.Fatalf("one code raised for two different fields is two branches. Reporting it as a "+
				"duplicate costs the author the only thing that tells the branches apart, and the "+
				"documented way to silence it is to merge them: %s", i.Message)
		}
	}
}

func TestTwoIdenticalFailuresAreStillADuplicate(t *testing.T) {
	issues := lintFailures(t, []Failure{
		{Code: 1105, Reason: "MissingField", Field: "name", When: "name is empty"},
		{Code: 1105, Reason: "MissingField", Field: "name", When: "name is empty"},
	})
	found := ""
	for _, i := range issues {
		if strings.Contains(i.Message, "repeats") {
			found = i.Message
		}
	}
	if found == "" {
		t.Fatal("the same branch written twice is still a duplicate and must still be reported")
	}
	if !strings.Contains(found, "on field name") {
		t.Errorf("the warning should name the field that collided: %q", found)
	}
}
