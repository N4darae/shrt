package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

const overlayWithNulls = `apiVersion: shrt/contract/v1
domain: test
rpcs:
    shrt.test.v1.ThingService/Create:
        summary: Create a thing.
        required: [NONE]
        fields:
            name:
    shrt.test.v1.ThingService/Fetch:
`

func TestAnEmptyContractEntryLintsInsteadOfCrashing(t *testing.T) {
	o, err := contract.LoadOverlayBytes("test.yaml", []byte(overlayWithNulls))
	if err != nil {
		t.Fatal(err)
	}
	lib := contract.NewLibrary([]*contract.Overlay{o})

	issues := contract.LintAll(lib, catalogtest.New(), nil)
	found := 0
	for _, i := range issues {
		if strings.Contains(i.Message, "entry is empty") {
			found++
			if i.Severity != contract.SeverityError {
				t.Errorf("empty entry reported as %q, want error: it is unfillable as written", i.Severity)
			}
		}
	}
	if found != 2 {
		t.Errorf("want 2 empty entries reported (one rpc, one field), got %d from %d issue(s)", found, len(issues))
	}
}

func TestPlanDoesNotPanicOnAnEmptyContractEntry(t *testing.T) {
	o, err := contract.LoadOverlayBytes("test.yaml", []byte(overlayWithNulls))
	if err != nil {
		t.Fatal(err)
	}
	lib := contract.NewLibrary([]*contract.Overlay{o})

	p, err := contract.BuildPlan("shrt.test.v1.ThingService/Create", lib, catalogtest.New(), "t")
	if err != nil {
		t.Fatalf("BuildPlan returned an error rather than panicking, which is fine, but it was: %v", err)
	}
	if _, err := p.YAML(); err != nil {
		t.Fatalf("YAML: %v", err)
	}
}
