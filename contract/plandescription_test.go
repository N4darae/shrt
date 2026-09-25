package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func TestPlanDescriptionIsTruthfulWithoutAContract(t *testing.T) {
	plan, err := contract.BuildPlan(shopConfirmOrder, contract.NewLibrary(nil), catalogtest.Shop(), "bare")
	if err != nil {
		t.Fatal(err)
	}
	d := plan.Chain.Description
	if strings.Contains(d, "composed from the contract dependency graph") || !strings.Contains(d, "no contract") {
		t.Fatalf("with no contract there is no dependency graph to compose from, got description %q", d)
	}
	if !strings.Contains(d, "ConfirmOrder") {
		t.Fatalf("the description must still name the target: %q", d)
	}

	plan, err = contract.BuildPlan(shopConfirmOrder, cancelFlowLibrary(t), catalogtest.Shop(), "curated")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Chain.Description, "composed from the contract dependency graph") {
		t.Fatalf("a plan built from contracts says so: %q", plan.Chain.Description)
	}
}
