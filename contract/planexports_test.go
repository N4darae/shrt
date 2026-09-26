package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func TestAPlannedChainExportsNothingNoStepReads(t *testing.T) {
	plan, err := contract.BuildPlan("ThingService/Fetch", libraryFrom(t, thingOverlay), catalogtest.New(), "thing-fetch")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	raw, err := plan.YAML()
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "${create.id}") {
		t.Fatalf("fetch reads create's response directly:\n%s", text)
	}
	if strings.Contains(text, "export:") || strings.Contains(text, "create_id") {
		t.Fatalf("an export no step reads is noise in the written chain:\n%s", text)
	}
}
